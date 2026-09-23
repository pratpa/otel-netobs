// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

package netboxobserver

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/collector/extension"
	"go.uber.org/zap"

	"github.com/pratpa/otel-netobs/extension/observer/netboxobserver/internal/metadata"
)

// testSettings builds the minimum the extension actually reads. newObserver
// touches only the logger, so the remaining fields are left at their zero
// values rather than pulled from a helper whose signature moves between
// collector releases.
func testSettings() extension.Settings {
	set := extension.Settings{}
	set.Logger = zap.NewNop()
	return set
}

func TestNewFactoryType(t *testing.T) {
	f := NewFactory()
	if f == nil {
		t.Fatal("NewFactory() returned nil")
	}
	if got, want := f.Type(), metadata.Type; got != want {
		t.Errorf("Type() = %q, want %q", got, want)
	}
}

func TestCreateDefaultConfig(t *testing.T) {
	cfg, ok := NewFactory().CreateDefaultConfig().(*Config)
	if !ok {
		t.Fatalf("CreateDefaultConfig() returned %T, want *Config", NewFactory().CreateDefaultConfig())
	}

	if cfg.RefreshInterval != 5*time.Minute {
		t.Errorf("RefreshInterval = %v, want 5m", cfg.RefreshInterval)
	}
	if cfg.Timeout != 30*time.Second {
		t.Errorf("Timeout = %v, want 30s", cfg.Timeout)
	}

	want := []string{"active", "failed", "offline", "decommissioning"}
	if len(cfg.Statuses) != len(want) {
		t.Fatalf("Statuses = %v, want %v", cfg.Statuses, want)
	}
	for i := range want {
		if cfg.Statuses[i] != want[i] {
			t.Errorf("Statuses[%d] = %q, want %q", i, cfg.Statuses[i], want[i])
		}
	}
}

// A device being built is not expected to answer. Collecting `staged` would
// create receivers that log timeouts until someone finished the work, so its
// absence from the default is deliberate rather than an oversight.
func TestCreateDefaultConfigExcludesStaged(t *testing.T) {
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	for _, s := range cfg.Statuses {
		if s == "staged" {
			t.Fatal("staged is collected by default; devices under construction would log timeouts")
		}
	}
}

// The default config carries no endpoint or token, so it must not validate on
// its own - the collector should refuse to start rather than poll nothing.
func TestCreateDefaultConfigDoesNotValidate(t *testing.T) {
	if err := NewFactory().CreateDefaultConfig().(*Config).Validate(); err == nil {
		t.Fatal("default config validates without an endpoint or token")
	}
}

func TestCreateExtension(t *testing.T) {
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	cfg.Endpoint = "https://netbox.example.com"
	cfg.Token = "test-token"

	ext, err := createExtension(context.Background(), testSettings(), cfg)
	if err != nil {
		t.Fatalf("createExtension: %v", err)
	}
	if ext == nil {
		t.Fatal("createExtension returned a nil extension")
	}
}

// Shutdown must be safe when the host never started watching - the collector
// calls it on any startup failure, including one in another component.
func TestShutdownWithoutStart(t *testing.T) {
	cfg := NewFactory().CreateDefaultConfig().(*Config)
	cfg.Endpoint = "https://netbox.example.com"
	cfg.Token = "test-token"

	ext, err := createExtension(context.Background(), testSettings(), cfg)
	if err != nil {
		t.Fatalf("createExtension: %v", err)
	}
	if err := ext.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
}
