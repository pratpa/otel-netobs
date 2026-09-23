# OpenTelemetry Collector components for network observability

OpenTelemetry Collector receivers and extensions for monitoring multi-vendor
data centre network estates driven by [NetBox](https://netbox.dev) as the
source of truth.

These components are built and run in production against a multi-vendor
estate — Cisco NX-OS and ACI, Cisco ASA and FTD, Cisco IOS-XE, and Palo Alto
PAN-OS — and are maintained here while upstream donation to
[opentelemetry-collector-contrib][contrib] is pursued.

> **Status: pre-donation.** Every component here is usable today via the
> [OpenTelemetry Collector Builder][ocb]. None has been accepted upstream.
> See [Upstream intent](#upstream-intent) below.

[contrib]: https://github.com/open-telemetry/opentelemetry-collector-contrib
[ocb]: https://github.com/open-telemetry/opentelemetry-collector/tree/main/cmd/builder

## Why these exist

The Collector's dynamic-configuration story is built for cloud-native
estates. Every observer shipped in contrib — `k8sobserver`, `dockerobserver`,
`ecsobserver`, `ecstaskobserver`, `hostobserver` — discovers workloads on a
scheduler or a local host. Nothing discovers *physical network devices from a
source of truth*.

That leaves operators of physical estates generating static collector
configuration from scripts, which is exactly the toil the `receiver_creator`
pattern was designed to remove. `netboxobserver` closes that gap: NetBox
becomes the endpoint source, and receivers are built and torn down as devices
are added, fail, or are decommissioned — with no collector restart.

The receivers exist for a simpler reason: the platforms in this estate have no
native Collector support. NX-API, PAN-OS XML API and the ASA/FTD SNMP dialects
were each either absent from contrib or not modelled in a way these devices
answer.

## Components

### Extensions

| Component | Type | Purpose |
| --- | --- | --- |
| [`netboxobserver`](extension/observer/netboxobserver) | observer | Emits `receiver_creator` endpoints from NetBox devices, filtered by status, role, site and tags. |
| [`netboxdiscoveryextension`](extension/netboxdiscoveryextension) | extension | Sweeps configured prefixes and records responding hosts as `ipam.ipaddress` objects in NetBox. |
| [`netboxproberextension`](extension/netboxproberextension) | extension | Probes devices over SNMP and NX-API and maintains reachability tags in NetBox. |

### Receivers

| Component | Type | Purpose |
| --- | --- | --- |
| [`nxapireceiver`](receiver/nxapireceiver) | receiver | Cisco NX-OS metrics over the NX-API JSON-RPC interface, including CDP and LLDP neighbours. |
| [`panosreceiver`](receiver/panosreceiver) | receiver | Palo Alto PAN-OS metrics over the XML API: real device uptime, management CPU, HA state and chassis environmentals, none of which SNMP reports usefully on PAN-OS. |
| [`asareceiver`](receiver/asareceiver) | receiver | Cisco ASA IPsec tunnel metrics, decoding `cipSecTunRemoteAddr` — a packed 4-byte address `snmpreceiver` renders as UTF-8, collapsing distinct tunnels into a handful of series. |
| [`acireceiver`](receiver/acireceiver) | receiver | Cisco ACI fabric health, node state, faults and tenant health from APIC. |
| [`netboxreceiver`](receiver/netboxreceiver) | receiver | NetBox inventory as metrics — what *should* be collected, as the denominator for "is everything reporting". |

## Repository layout

The directory structure deliberately mirrors `opentelemetry-collector-contrib`,
so that donating a component upstream is a directory move rather than a
restructure:

```
extension/
  observer/
    netboxobserver/
  netboxdiscoveryextension/
  netboxproberextension/
receiver/
  nxapireceiver/
  panosreceiver/
  asareceiver/
  acireceiver/
  netboxreceiver/
```

Only `netboxobserver` is present today; the rest land as each is prepared for
publication.

Each component is its own Go module, as contrib requires. A `go.work` at the
repository root ties them together for local development.

## Using these components

Add the ones you need to an OCB `builder-config.yaml`:

```yaml
dist:
  name: otelcol-netobs
  output_path: ./bin

extensions:
  - gomod: github.com/pratpa/otel-netobs/extension/observer/netboxobserver v0.1.0

receivers:
  - gomod: github.com/pratpa/otel-netobs/receiver/nxapireceiver v0.1.0
```

Then:

```sh
builder --config builder-config.yaml
```

Each component's own README documents its configuration.

### Secrets

No component accepts a file path or a secret-store reference. Every credential
is a plain string, resolved by the Collector's own confmap providers:

```yaml
extensions:
  netbox_observer:
    token: ${env:NETBOX_TOKEN}
```

This keeps secret handling entirely in the Collector's hands — `${env:...}`
today, a Vault or cloud secret-manager provider later — with no component
changes required.

## Upstream intent

The intent is to donate these components to
[opentelemetry-collector-contrib][contrib] under its
[component donation process][donation]. `netboxobserver` is the lead
candidate.

Donating requires a sponsor (an approver or maintainer from another company)
and three or more OpenTelemetry organization members willing to act as code
owners. If you run a physical network estate on the Collector and would be
interested in either role, please open an issue.

One upstream change is a prerequisite for `netboxobserver` to be idiomatic:
`receiver_creator` validates endpoint types against a closed list in
[`rules.go`][rules] and a closed switch in `config.go`, with no extension
point for observers outside the eight built-in types. Until that changes,
observers for non-cloud-native estates must reuse an existing endpoint type.

[donation]: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/docs/new-components.md
[rules]: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/receivercreator/rules.go

## Contributing

Issues and pull requests are welcome. Please run `make test` and `make lint`
before opening a PR.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
