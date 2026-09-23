// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

package metadata // import "github.com/pratpa/otel-netobs/extension/observer/netboxobserver/internal/metadata"

import (
	"go.opentelemetry.io/collector/component"
)

var (
	Type = component.MustNewType("netbox_observer")
)

const (
	ExtensionStability = component.StabilityLevelDevelopment
)
