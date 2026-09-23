# Draft: new component donation issue for `netboxobserver`

Repository: `open-telemetry/opentelemetry-collector-contrib`
Template: **New component** (applies labels `Sponsor Needed`, `needs triage`)
Suggested title:

> New component: NetBox observer

Two required fields are not yet answerable — Code Owners needs three
OpenTelemetry members, and the implementation URL must be publicly
reachable. Everything else below is ready to paste.

---

## Building and distributing outside this repository

- [x] I understand that the OpenTelemetry Collector has a pluggable
  architecture, and that I can build and distribute this component without
  it being part of this repository.

## Existing component implementation

https://github.com/pratpa/otel-netobs/tree/main/extension/observer/netboxobserver

## Components covering similar use cases

Five observers ship in this repository today:

- `k8sobserver` — Kubernetes pods, services, ingresses and nodes
- `dockerobserver` — Docker containers
- `ecsobserver` / `cfgardenobserver` — ECS tasks and Cloud Foundry Garden containers
- `hostobserver` — listening ports on the local host

All five discover workloads on a container scheduler or the local machine.
None discovers infrastructure recorded in an external source of truth, which
is how physical estates — network devices, and arguably any inventoried
hardware — describe what exists.

Outside this repository I am not aware of an observer that reads an inventory
system. The closest existing patterns are Prometheus' HTTP and file service
discovery, which solve the same problem for a different collector.

## The purpose and use-cases of the component

`netboxobserver` polls [NetBox](https://netbox.dev) and reports each device as
an endpoint, so `receiver_creator` can instantiate that device's receivers
from a static per-platform template.

The problem it solves is the one `receiver_creator` already solves for pods,
applied to hardware. Monitoring a physical estate without it means generating
collector configuration from an inventory system with an external script: one
receiver block per device per metric group. On the estate this was built for
that was a 6,700-line generated configuration, a generator to produce it, and
a collector restart on every inventory change — roughly 120 receivers torn
down and rebuilt to add a single switch.

With the observer, NetBox is polled directly and receivers follow the
inventory. A device added to NetBox is collected within the refresh interval;
a device decommissioned stops being collected. No regeneration, no restart.
The configuration becomes one template per platform rather than one block per
device.

Beyond the reduction in configuration, the device's inventory attributes —
site, role, model, platform, tags — become available to rules and to resource
attributes, so metrics carry their inventory identity without that identity
being duplicated into the collector configuration.

Concretely, the tag-based exclusion is what makes this practical at scale:

```yaml
rule: type == "port" && platform == "nxos" && !("snmp-unreachable" in tags)
```

A device tagged unreachable simply stops having receivers created for it,
rather than logging a timeout every scrape interval indefinitely.

It is read-only: it creates nothing in NetBox and modifies nothing there.

## Example configuration for the component

```yaml
extensions:
  netbox_observer:
    endpoint: ${env:NETBOX_URL}
    token: ${env:NETBOX_TOKEN}
    refresh_interval: 5m
    statuses: [active, failed, offline, decommissioning]

receivers:
  receiver_creator:
    watch_observers: [netbox_observer]

    resource_attributes:
      port:
        hostname: '`name`'
        platform: '`platform`'
        region: '`site`'
        source: '`ip`'
        device_role: '`role`'

    receivers:
      snmp/nxos:
        rule: type == "port" && platform == "nxos" && !("snmp-unreachable" in tags)
        config:
          endpoint: udp://`endpoint`:161
          version: v2c
          community: ${env:SNMP_COMMUNITY}

service:
  extensions: [netbox_observer]
  pipelines:
    metrics:
      receivers: [receiver_creator]
      exporters: [prometheusremotewrite]
```

Configuration reference:

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `endpoint` | string | — | Required. NetBox base URL. |
| `token` | string | — | Required. NetBox API token; read-only is sufficient. |
| `refresh_interval` | duration | `5m` | How often NetBox is polled. |
| `timeout` | duration | `30s` | Per-request HTTP timeout. |
| `statuses` | []string | `[active, failed, offline, decommissioning]` | Device statuses to collect. |
| `insecure_skip_verify` | bool | `false` | Skip TLS verification of the NetBox certificate. |

## Telemetry data types supported

None directly. An observer emits endpoints rather than telemetry.

In practice it drives metrics, since the receivers it instantiates for network
devices are `snmpreceiver` and similar. Nothing in the component constrains
that — any receiver `receiver_creator` can build from a `port` endpoint works,
across any signal.

## Code Owners

*(To be completed — requires three or more OpenTelemetry members, at least one
an approver or maintainer.)*

@pratpa

## Sponsor

*(To be completed.)*

## Additional context

One upstream limitation is worth raising alongside this, and may be worth
resolving first.

Devices are reported as `observer.PortType` rather than a device type of
their own. `observer.EndpointType` is a plain string and the observer package
accepts any value, but `receiver_creator` validates it against a closed list
of the eight built-in types in two places — a regex in `rules.go` and a switch
in `config.go` — so a custom type is rejected before a rule is evaluated.

`port` is defensible (an SNMP agent on UDP 161 is a port) and rules
discriminate on `platform`, which is what matters in practice. But it means
any observer for a non-cloud-native estate must borrow a type that does not
describe what it reports. Filed separately as <issue link>.

---

## Notes (not part of the issue)

- The implementation URL must resolve publicly before this can be filed; the
  template requires it and reviewers will follow it immediately.
- Code Owners is the binding constraint, not the sponsor field. A sponsor is
  optional on the template — the issue sits with a `Sponsor Needed` label
  until one appears — but three OpenTelemetry members must be named.
- File the `receiver_creator` issue first and link it here. It is a smaller
  ask, needs no sponsor, and gives the maintainers who would review this
  donation prior context on the use case.

### A short introduction for `#otel-collector` on CNCF Slack

Not an ask — just making the use case visible. Post it after the
`receiver_creator` issue exists so there is something concrete to point at.

> Hi all — I run network observability for a multi-vendor data centre estate
> (NX-OS, ACI, ASA/FTD, IOS-XE, PAN-OS) and we've moved it from Telegraf onto
> the Collector, with NetBox as the source of truth.
>
> To avoid generating a per-device config, I wrote an observer that reports
> NetBox devices as endpoints so `receiver_creator` builds receivers from
> per-platform templates instead. It's been running in production for a while
> and replaced a 6,700-line generated config.
>
> One thing I ran into: `receiver_creator` validates endpoint types against a
> closed list of the eight built-in types, so an observer outside this repo
> can't declare a type of its own even though `observer.EndpointType` is just
> a string. I've written that up at <issue link>.
>
> Code is at <repo link> if anyone with a physical estate finds it useful.
> Happy to take it through the donation process if there's appetite, but
> mainly wanted to surface the limitation.
