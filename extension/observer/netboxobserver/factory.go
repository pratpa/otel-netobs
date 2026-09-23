// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

package netboxobserver // import "github.com/pratpa/otel-netobs/extension/observer/netboxobserver"

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/extension"

	"github.com/pratpa/otel-netobs/extension/observer/netboxobserver/internal/metadata"
)

// NewFactory creates a factory for the NetBox observer extension.
func NewFactory() extension.Factory {
	return extension.NewFactory(
		metadata.Type,
		createDefaultConfig,
		createExtension,
		metadata.ExtensionStability,
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		RefreshInterval: 5 * time.Minute,
		Timeout:         30 * time.Second,
		// Matches what the generator collected. `staged` is deliberately
		// absent - see the Config docstring.
		Statuses: []string{"active", "failed", "offline", "decommissioning"},
	}
}

func createExtension(
	_ context.Context,
	params extension.Settings,
	cfg component.Config,
) (extension.Extension, error) {
	return newObserver(params, cfg.(*Config))
}
