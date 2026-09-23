// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

package netboxobserver // import "github.com/pratpa/otel-netobs/extension/observer/netboxobserver"

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer/endpointswatcher"
)

// DeviceType is the endpoint type this observer emits.
//
// observer.EndpointType is a plain string rather than a closed enum, so a new
// type needs no change to the observer package. Rules match on it:
//
//	rule: type == "netbox.device" && platform == "nxos"
//
// Devices are reported as PortType, not as a type of their own.
//
// A custom EndpointType looked correct - observer.EndpointType is a plain
// string, and nothing in the observer package objects. receiver_creator does,
// in two places (line numbers as of contrib v0.158.0):
//
//	config.go:109  resource_attributes validates the endpoint type against a
//	               closed switch over the eight built-in types
//	rules.go:35    the rule string must MATCH A REGEX built from those same
//	               eight literals, so a custom type is rejected before the
//	               rule is ever evaluated
//
// Tracked upstream; see the repository README.
//
// PortType is the honest choice among them: an SNMP agent on UDP 161 is a
// port. Rules discriminate on the platform attribute, which is what actually
// matters.
//
// This only changes what Type() REPORTS. Device implements EndpointDetails
// itself, so Env() below is entirely ours - observer.Port's fields and env
// keys are not involved.
const DeviceType = observer.PortType

type netboxObserver struct {
	*endpointswatcher.EndpointsWatcher
}

var _ extension.Extension = (*netboxObserver)(nil)

func newObserver(params extension.Settings, cfg *Config) (extension.Extension, error) {
	lister := &endpointsLister{
		logger: params.Logger,
		cfg:    cfg,
		http: &http.Client{
			Timeout: cfg.Timeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: cfg.InsecureSkipVerify},
			},
		},
	}
	return &netboxObserver{
		EndpointsWatcher: endpointswatcher.New(lister, cfg.RefreshInterval, params.Logger),
	}, nil
}

func (*netboxObserver) Start(context.Context, component.Host) error { return nil }

func (o *netboxObserver) Shutdown(context.Context) error {
	o.StopListAndWatch()
	return nil
}

// Device is what a rule sees. Every field is available as a variable in the
// rule expression and for backtick expansion in the receiver template.
type Device struct {
	Name     string
	Platform string
	Site     string
	Role     string
	Model    string
	Serial   string
	Status   string
	IP       string
	Tags     []string
}

// Env exposes the device to the rule engine.
//
// NOTE the framework also sets `endpoint`, `type`, `id`, `host` and `port`
// from the Endpoint itself - see observer.Endpoint.Env(). None of the keys
// here may collide with those, or the framework's value silently wins.
func (d *Device) Env() observer.EndpointEnv {
	return observer.EndpointEnv{
		"name":     d.Name,
		"platform": d.Platform,
		"site":     d.Site,
		"role":     d.Role,
		"model":    d.Model,
		"serial":   d.Serial,
		"status":   d.Status,
		"ip":       d.IP,
		// A list, so a rule can write:
		//   !("snmp-unreachable" in tags)
		// which is how unreachable devices are excluded from polling.
		"tags": d.Tags,
	}
}

func (*Device) Type() observer.EndpointType { return DeviceType }

var _ observer.EndpointDetails = (*Device)(nil)

type endpointsLister struct {
	logger *zap.Logger
	cfg    *Config
	http   *http.Client
}

type nbPage struct {
	Next    string            `json:"next"`
	Results []json.RawMessage `json:"results"`
}

type nbDevice struct {
	Name   string `json:"name"`
	Serial string `json:"serial"`
	Status struct {
		Value string `json:"value"`
	} `json:"status"`
	PrimaryIP4 *struct {
		Address string `json:"address"`
	} `json:"primary_ip4"`
	Site *struct {
		Slug string `json:"slug"`
	} `json:"site"`
	Role *struct {
		Slug string `json:"slug"`
	} `json:"role"`
	Platform *struct {
		Slug string `json:"slug"`
	} `json:"platform"`
	DeviceType *struct {
		Model string `json:"model"`
	} `json:"device_type"`
	Tags []struct {
		Slug string `json:"slug"`
	} `json:"tags"`
}

func (l *endpointsLister) get(url string) (*nbPage, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Token "+l.cfg.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := l.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("netbox returned HTTP %d", resp.StatusCode)
	}
	var p nbPage
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListEndpoints returns one endpoint per device NetBox says should be
// collected.
//
// RETURNING NIL ON ERROR IS DELIBERATE, and it is the subtle part of this
// component. EndpointsWatcher diffs against the previous result, so an empty
// list means "every device disappeared" and every receiver would be torn
// down. A NetBox blip would stop all collection.
//
// Returning nil instead leaves the previous endpoint set in place - the
// watcher treats it as no change. Collection continues on the last known good
// device list until NetBox answers again.
func (l *endpointsLister) ListEndpoints() []observer.Endpoint {
	var devices []nbDevice

	for _, status := range l.cfg.Statuses {
		url := fmt.Sprintf("%s/api/dcim/devices/?status=%s&limit=200",
			strings.TrimRight(l.cfg.Endpoint, "/"), status)
		// Bounded: this estate has tens of devices, so more than 20 pages
		// means a filter is wrong and we are walking the whole database.
		for i := 0; url != "" && i < 20; i++ {
			p, err := l.get(url)
			if err != nil {
				l.logger.Error("netbox query failed - keeping the previous endpoint set",
					zap.String("status", status), zap.Error(err))
				return nil
			}
			for _, raw := range p.Results {
				var d nbDevice
				if json.Unmarshal(raw, &d) == nil {
					devices = append(devices, d)
				}
			}
			url = p.Next
		}
	}

	if len(devices) == 0 {
		l.logger.Warn("netbox returned no devices - keeping the previous endpoint set")
		return nil
	}

	endpoints := make([]observer.Endpoint, 0, len(devices))
	for _, d := range devices {
		if d.Name == "" || d.PrimaryIP4 == nil {
			continue
		}
		ip := strings.SplitN(d.PrimaryIP4.Address, "/", 2)[0]
		if ip == "" {
			continue
		}

		dev := &Device{
			Name:   d.Name,
			Serial: d.Serial,
			Status: d.Status.Value,
			IP:     ip,
		}
		if d.Platform != nil {
			dev.Platform = d.Platform.Slug
		}
		if d.Site != nil {
			dev.Site = d.Site.Slug
		}
		if d.Role != nil {
			dev.Role = d.Role.Slug
		}
		if d.DeviceType != nil {
			dev.Model = d.DeviceType.Model
		}
		// ALWAYS a slice, never nil. append to a nil slice leaves it nil
		// when there is nothing to append, and the env marshals as
		// tags:null. A rule testing membership then fails against null
		// and the device is silently skipped - which is why only the 10
		// devices that happened to carry tags were ever matched.
		dev.Tags = make([]string, 0, len(d.Tags))
		for _, t := range d.Tags {
			dev.Tags = append(dev.Tags, t.Slug)
		}

		endpoints = append(endpoints, observer.Endpoint{
			// The ID must be stable across polls. Using the device NAME means
			// a device that is renamed is seen as one removed and one added,
			// which is correct - its metrics change identity anyway.
			//
			// It must NOT be the IP: two devices can briefly share one during
			// a renumber, and the watcher would treat them as the same
			// endpoint.
			ID:      observer.EndpointID(d.Name),
			Target:  ip,
			Details: dev,
		})
	}

	l.logger.Info("netbox endpoints", zap.Int("count", len(endpoints)))
	return endpoints
}

// Config defines configuration for the NetBox observer.
type Config struct {
	// NetBox base URL, e.g. https://netbox.example.com
	Endpoint string `mapstructure:"endpoint"`
	// A READ-ONLY API token. This component never writes.
	Token string `mapstructure:"token"`

	// How often NetBox is polled. Five minutes rather than seconds: device
	// inventory changes when a human edits NetBox, and each poll is a full
	// device list.
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	Timeout         time.Duration `mapstructure:"timeout"`

	// Device statuses to collect. `staged` is absent by default: a device
	// being built is not expected to answer, and creating receivers for it
	// would log errors until someone finished the work.
	Statuses []string `mapstructure:"statuses"`

	InsecureSkipVerify bool `mapstructure:"insecure_skip_verify"`

	_ struct{}
}

func (c *Config) Validate() error {
	if c.Endpoint == "" {
		return fmt.Errorf("endpoint is required")
	}
	if c.Token == "" {
		return fmt.Errorf("token is required")
	}
	return nil
}
