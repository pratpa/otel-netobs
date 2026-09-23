// Copyright Qualys, Inc.
// SPDX-License-Identifier: Apache-2.0

//go:generate mdatagen metadata.yaml

// Package netboxobserver discovers network devices from NetBox and reports
// them as endpoints for the receiver_creator.
//
// # WHY THIS EXISTS
//
// Without it, every device's receivers must be written into the collector
// config ahead of time. On the estate this was built for, that meant a
// 6,700-line generated config, a Python generator to produce it, and a full
// collector restart whenever NetBox changed - roughly 120 receivers torn down
// and rebuilt to add one switch.
//
// With it, NetBox is polled directly, each device becomes an endpoint, and
// receiver_creator instantiates receivers from a handful of static templates -
// one per platform rather than one per device. A device added to NetBox starts
// collecting within the refresh interval, with no config regeneration and no
// restart.
//
// # WHAT IT DOES NOT DO
//
// It is read-only and it discovers nothing itself. NetBox is the source of
// truth; a separate sweep finds devices and a separate prober maintains their
// reachability tags. This observer only reads what those have already written.
package netboxobserver // import "github.com/pratpa/otel-netobs/extension/observer/netboxobserver"
