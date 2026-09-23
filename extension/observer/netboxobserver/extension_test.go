// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

package netboxobserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-telemetry/opentelemetry-collector-contrib/extension/observer"
	"go.uber.org/zap"
)

// ---------------------------------------------------------------- fixtures

const (
	deviceFull = `{
		"name": "sw-01",
		"serial": "ABC123",
		"status": {"value": "active"},
		"primary_ip4": {"address": "10.1.1.1/24"},
		"site": {"slug": "sjc01"},
		"role": {"slug": "access-switch"},
		"platform": {"slug": "nxos"},
		"device_type": {"model": "N9K-C93180YC-EX"},
		"tags": [{"slug": "snmp-unreachable"}, {"slug": "managed"}]
	}`

	// The case that matters: a device carrying no tags at all.
	deviceNoTags = `{
		"name": "sw-02",
		"status": {"value": "active"},
		"primary_ip4": {"address": "10.1.1.2/24"},
		"platform": {"slug": "nxos"},
		"tags": []
	}`

	// No primary_ip4 at all - nothing to point a receiver at.
	deviceNoIP = `{
		"name": "sw-03",
		"status": {"value": "active"},
		"platform": {"slug": "nxos"},
		"tags": []
	}`

	// Present in NetBox but unnamed.
	deviceNoName = `{
		"name": "",
		"status": {"value": "active"},
		"primary_ip4": {"address": "10.1.1.4/24"},
		"tags": []
	}`

	// Minimal device: every optional relation absent.
	deviceBare = `{
		"name": "sw-05",
		"status": {"value": "offline"},
		"primary_ip4": {"address": "10.1.1.5/32"}
	}`
)

// devicesPage renders a NetBox list response. An empty next yields JSON null,
// which decodes into the empty string and ends pagination.
func devicesPage(next string, devices ...string) string {
	nextField := "null"
	if next != "" {
		nextField = fmt.Sprintf("%q", next)
	}
	return fmt.Sprintf(`{"next": %s, "results": [%s]}`, nextField, strings.Join(devices, ","))
}

func newTestLister(srv *httptest.Server, statuses ...string) *endpointsLister {
	if len(statuses) == 0 {
		statuses = []string{"active"}
	}
	return &endpointsLister{
		logger: zap.NewNop(),
		cfg: &Config{
			Endpoint: srv.URL,
			Token:    "test-token",
			Statuses: statuses,
			Timeout:  5 * time.Second,
		},
		http: srv.Client(),
	}
}

// serve starts a test server whose handler is h, registering cleanup.
func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func deviceByName(t *testing.T, eps []observer.Endpoint, name string) *Device {
	t.Helper()
	for _, ep := range eps {
		d, ok := ep.Details.(*Device)
		if !ok {
			t.Fatalf("Details is %T, want *Device", ep.Details)
		}
		if d.Name == name {
			return d
		}
	}
	t.Fatalf("no endpoint named %q in %d endpoints", name, len(eps))
	return nil
}

// ------------------------------------------------------- tags are never nil

// A nil slice marshals as `null`, and a rule written as
// !("snmp-unreachable" in tags) then evaluates against null rather than a
// list. The rule fails, the device is silently skipped, and only devices that
// happen to carry tags are ever matched. This is a regression test for that.
func TestListEndpointsTagsAreNeverNil(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage("", deviceNoTags, deviceBare))
	})

	eps := newTestLister(srv).ListEndpoints()
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}

	for _, name := range []string{"sw-02", "sw-05"} {
		d := deviceByName(t, eps, name)
		if d.Tags == nil {
			t.Errorf("%s: Tags is nil", name)
			continue
		}
		b, err := json.Marshal(d.Env()["tags"])
		if err != nil {
			t.Fatalf("%s: marshal tags: %v", name, err)
		}
		if string(b) != "[]" {
			t.Errorf("%s: tags marshalled as %s, want []", name, b)
		}
	}
}

func TestListEndpointsTagsArePopulated(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage("", deviceFull))
	})

	d := deviceByName(t, newTestLister(srv).ListEndpoints(), "sw-01")
	want := []string{"snmp-unreachable", "managed"}
	if len(d.Tags) != len(want) {
		t.Fatalf("got tags %v, want %v", d.Tags, want)
	}
	for i := range want {
		if d.Tags[i] != want[i] {
			t.Errorf("tag %d = %q, want %q", i, d.Tags[i], want[i])
		}
	}
}

// ------------------------------------------ failure keeps the previous set

// An empty list means "every device disappeared" to EndpointsWatcher, which
// would tear down every receiver. Returning nil is how a NetBox blip is made
// harmless, so both failure paths must return nil rather than an empty slice.
func TestListEndpointsReturnsNilOnHTTPError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if eps := newTestLister(srv).ListEndpoints(); eps != nil {
		t.Fatalf("got %#v, want nil - an empty slice tears down every receiver", eps)
	}
}

func TestListEndpointsReturnsNilOnMalformedJSON(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, `{"next": null, "results":`)
	})

	if eps := newTestLister(srv).ListEndpoints(); eps != nil {
		t.Fatalf("got %#v, want nil", eps)
	}
}

func TestListEndpointsReturnsNilWhenNetboxHasNoDevices(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage(""))
	})

	if eps := newTestLister(srv).ListEndpoints(); eps != nil {
		t.Fatalf("got %#v, want nil", eps)
	}
}

// ------------------------------------------------------------- field mapping

func TestListEndpointsMapsAllFields(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage("", deviceFull))
	})

	eps := newTestLister(srv).ListEndpoints()
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	ep := eps[0]

	// The ID must be the device name. Using the IP would collide when two
	// devices briefly share one during a renumber.
	if ep.ID != observer.EndpointID("sw-01") {
		t.Errorf("ID = %q, want sw-01", ep.ID)
	}
	// Target is the bare address: the CIDR mask NetBox returns must be gone.
	if ep.Target != "10.1.1.1" {
		t.Errorf("Target = %q, want 10.1.1.1 (mask must be stripped)", ep.Target)
	}

	d := ep.Details.(*Device)
	for _, c := range []struct{ field, got, want string }{
		{"Name", d.Name, "sw-01"},
		{"Serial", d.Serial, "ABC123"},
		{"Status", d.Status, "active"},
		{"IP", d.IP, "10.1.1.1"},
		{"Site", d.Site, "sjc01"},
		{"Role", d.Role, "access-switch"},
		{"Platform", d.Platform, "nxos"},
		{"Model", d.Model, "N9K-C93180YC-EX"},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.field, c.got, c.want)
		}
	}
}

// Every optional relation is a pointer in the NetBox response. A device with
// none of them must still produce an endpoint rather than panic.
func TestListEndpointsToleratesMissingRelations(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage("", deviceBare))
	})

	d := deviceByName(t, newTestLister(srv).ListEndpoints(), "sw-05")
	for _, c := range []struct{ field, got string }{
		{"Platform", d.Platform},
		{"Site", d.Site},
		{"Role", d.Role},
		{"Model", d.Model},
	} {
		if c.got != "" {
			t.Errorf("%s = %q, want empty", c.field, c.got)
		}
	}
	if d.IP != "10.1.1.5" {
		t.Errorf("IP = %q, want 10.1.1.5", d.IP)
	}
}

func TestListEndpointsSkipsUnusableDevices(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, devicesPage("", deviceFull, deviceNoIP, deviceNoName))
	})

	eps := newTestLister(srv).ListEndpoints()
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (no-IP and no-name devices are skipped)", len(eps))
	}
	if eps[0].ID != observer.EndpointID("sw-01") {
		t.Errorf("kept %q, want sw-01", eps[0].ID)
	}
}

// ---------------------------------------------------------------- requests

func TestListEndpointsSendsTokenAsAuthorizationHeader(t *testing.T) {
	var got string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		fmt.Fprint(w, devicesPage("", deviceFull))
	})

	newTestLister(srv).ListEndpoints()
	if want := "Token test-token"; got != want {
		t.Errorf("Authorization = %q, want %q", got, want)
	}
}

func TestListEndpointsQueriesEveryConfiguredStatus(t *testing.T) {
	var seen []string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.URL.Query().Get("status"))
		fmt.Fprint(w, devicesPage("", deviceFull))
	})

	newTestLister(srv, "active", "failed", "offline").ListEndpoints()
	want := []string{"active", "failed", "offline"}
	if len(seen) != len(want) {
		t.Fatalf("made %d requests %v, want %d", len(seen), seen, len(want))
	}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("request %d queried status %q, want %q", i, seen[i], want[i])
		}
	}
}

// A trailing slash on the configured endpoint must not produce a double
// slash in the request path.
func TestListEndpointsTrimsTrailingSlashFromEndpoint(t *testing.T) {
	var path string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		fmt.Fprint(w, devicesPage("", deviceFull))
	})

	l := newTestLister(srv)
	l.cfg.Endpoint = srv.URL + "/"
	l.ListEndpoints()

	if want := "/api/dcim/devices/"; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
}

// ---------------------------------------------------------------- paging

func TestListEndpointsFollowsPagination(t *testing.T) {
	var srv *httptest.Server
	var hits int
	srv = serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		if hits == 1 {
			fmt.Fprint(w, devicesPage(srv.URL+"/api/dcim/devices/?page=2", deviceFull))
			return
		}
		fmt.Fprint(w, devicesPage("", deviceNoTags))
	})

	eps := newTestLister(srv).ListEndpoints()
	if hits != 2 {
		t.Fatalf("made %d requests, want 2", hits)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2 across both pages", len(eps))
	}
}

// A NetBox filter that does not narrow anything would otherwise walk the whole
// database. The page loop is bounded; this pins the bound.
func TestListEndpointsStopsPagingAtTheBound(t *testing.T) {
	var srv *httptest.Server
	var hits int
	srv = serve(t, func(w http.ResponseWriter, _ *http.Request) {
		hits++
		fmt.Fprint(w, devicesPage(srv.URL+"/api/dcim/devices/?page=next", deviceFull))
	})

	newTestLister(srv).ListEndpoints()
	if hits != 20 {
		t.Fatalf("made %d requests, want 20 - the page loop must stay bounded", hits)
	}
}

// ------------------------------------------------------------------ Device

func TestDeviceEnvExposesEveryField(t *testing.T) {
	d := &Device{
		Name: "sw-01", Platform: "nxos", Site: "sjc01", Role: "access-switch",
		Model: "N9K", Serial: "ABC123", Status: "active", IP: "10.1.1.1",
		Tags: []string{"managed"},
	}
	env := d.Env()

	for k, want := range map[string]string{
		"name": "sw-01", "platform": "nxos", "site": "sjc01",
		"role": "access-switch", "model": "N9K", "serial": "ABC123",
		"status": "active", "ip": "10.1.1.1",
	} {
		got, ok := env[k]
		if !ok {
			t.Errorf("Env() is missing %q", k)
			continue
		}
		if got != want {
			t.Errorf("Env()[%q] = %v, want %q", k, got, want)
		}
	}

	tags, ok := env["tags"].([]string)
	if !ok {
		t.Fatalf("Env()[\"tags\"] is %T, want []string", env["tags"])
	}
	if len(tags) != 1 || tags[0] != "managed" {
		t.Errorf("Env()[\"tags\"] = %v, want [managed]", tags)
	}
}

// observer.Endpoint.Env() sets endpoint, type, id, host and port from the
// Endpoint itself. A key defined here under any of those names is silently
// overwritten by the framework, so the rule would read a value this component
// never set.
func TestDeviceEnvDoesNotShadowFrameworkKeys(t *testing.T) {
	env := (&Device{}).Env()
	for _, reserved := range []string{"endpoint", "type", "id", "host", "port"} {
		if _, clash := env[reserved]; clash {
			t.Errorf("Env() defines %q, which observer.Endpoint.Env() also sets", reserved)
		}
	}
}

// Devices are reported as PortType because receiver_creator validates the
// endpoint type against a closed list. If this ever becomes a custom type,
// every rule in every platform template has to change with it.
func TestDeviceTypeIsPortType(t *testing.T) {
	if got := (&Device{}).Type(); got != observer.PortType {
		t.Errorf("Type() = %q, want %q", got, observer.PortType)
	}
	if DeviceType != observer.PortType {
		t.Errorf("DeviceType = %q, want %q", DeviceType, observer.PortType)
	}
}

// ------------------------------------------------------------------ Config

func TestConfigValidate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"complete", Config{Endpoint: "https://netbox.example.com", Token: "t"}, false},
		{"no endpoint", Config{Token: "t"}, true},
		{"no token", Config{Endpoint: "https://netbox.example.com"}, true},
		{"empty", Config{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("want an error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("want no error, got %v", err)
			}
		})
	}
}
