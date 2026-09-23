# NetBox Observer

| Status | |
| --- | --- |
| Stability | development |
| Distributions | none |
| Issues | [open an issue](https://github.com/pratpa/otel-netobs/issues) |
| Code owners | [@pratpa](https://www.github.com/pratpa) |

An [observer] that discovers network devices from [NetBox] and reports each one
as an endpoint, so that [`receiver_creator`][rc] can instantiate that device's
receivers from a static per-platform template.

Every observer shipped in `opentelemetry-collector-contrib` discovers workloads
on a container scheduler or the local host. This one discovers physical devices
from a source of truth, which makes `receiver_creator`'s add/remove lifecycle
available to network estates: a device added to NetBox starts being collected
within the refresh interval, and a device removed stops — with no configuration
regeneration and no collector restart.

It is **read-only**. It discovers nothing itself and writes nothing back;
NetBox is the source of truth and this component only reads it.

[observer]: https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/extension/observer
[NetBox]: https://netbox.dev
[rc]: https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver/receivercreator

## Configuration

```yaml
extensions:
  netbox_observer:
    endpoint: ${env:NETBOX_URL}
    token: ${env:NETBOX_TOKEN}
    refresh_interval: 5m
    statuses: [active, failed, offline, decommissioning]
```

| Field | Type | Default | Description |
| --- | --- | --- | --- |
| `endpoint` | string | — | **Required.** NetBox base URL, e.g. `https://netbox.example.com`. No trailing slash needed; one is trimmed if present. |
| `token` | string | — | **Required.** NetBox API token. A **read-only** token is sufficient and is what you should use — this component never writes. |
| `refresh_interval` | duration | `5m` | How often NetBox is polled. Device inventory changes when a human edits NetBox, so seconds-scale polling buys nothing and each poll is a full device list. |
| `timeout` | duration | `30s` | Per-request HTTP timeout. |
| `statuses` | []string | `[active, failed, offline, decommissioning]` | NetBox device statuses to collect. One request is made per status. |
| `insecure_skip_verify` | bool | `false` | Skip TLS verification of the NetBox certificate. |

### Why `staged` is not collected by default

A device in `staged` is still being built and is not expected to answer.
Including it creates receivers that log a timeout on every scrape interval
until somebody finishes the work. Add it explicitly if you want that.

### The token is a secret

Supply it through the collector's own configuration resolution — `${env:...}`
as above, or any other [confmap provider]. The component takes a plain string
and deliberately has no `token_file` or secret-store settings of its own, so
moving from environment variables to a secret manager is a configuration
change rather than a code change.

[confmap provider]: https://github.com/open-telemetry/opentelemetry-collector/tree/main/confmap

## Using it with `receiver_creator`

Each device becomes one endpoint. Rules discriminate on the device's
attributes, and backticks expand them into the receiver template:

```yaml
extensions:
  netbox_observer:
    endpoint: ${env:NETBOX_URL}
    token: ${env:NETBOX_TOKEN}

receivers:
  receiver_creator:
    watch_observers: [netbox_observer]

    # One set of resource attributes covers every device on every platform,
    # taking the values from the endpoint rather than hardcoding them per
    # device.
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
          # ... metrics omitted

service:
  extensions: [netbox_observer]
  pipelines:
    metrics:
      receivers: [receiver_creator]
      exporters: [prometheusremotewrite]
```

### Rule variables

| Variable | Source |
| --- | --- |
| `name` | NetBox device name |
| `platform` | `platform.slug` |
| `site` | `site.slug` |
| `role` | `role.slug` |
| `model` | `device_type.model` |
| `serial` | Device serial |
| `status` | `status.value` — `active`, `failed`, `offline`, … |
| `ip` | `primary_ip4`, with the CIDR mask stripped |
| `tags` | List of tag slugs. Always a list, never null, so membership tests are safe on untagged devices. |

`endpoint`, `type`, `id`, `host` and `port` are set by the framework from the
Endpoint itself and are available in rules alongside the above.

## Endpoint type

Devices are reported as **`port`** (`observer.PortType`), not as a device type
of their own, so rules must begin `type == "port"`.

`observer.EndpointType` is a plain string and the observer package accepts any
value, but `receiver_creator` validates it against a closed list of the eight
built-in types in two places — a regex in `rules.go` and a switch in
`config.go`. A custom type is rejected before the rule is evaluated.

`port` is the honest choice among the eight: an SNMP agent on UDP 161 is a
port. Rules discriminate on `platform`, which is what actually matters. Only
the reported `Type()` is affected — `Device` implements `EndpointDetails`
itself, so none of `observer.Port`'s fields or env keys are involved.

Relaxing that validation upstream is tracked in the repository
[README](../../../README.md#upstream-intent).

## Behaviour worth knowing

**A NetBox outage does not tear down collection.** `ListEndpoints` returns nil
rather than an empty list when a query fails or returns nothing.
`EndpointsWatcher` diffs against the previous result, so an empty list would
read as "every device disappeared" and every receiver would be torn down.
Returning nil leaves the previous endpoint set in place and collection
continues against the last known good device list.

**Endpoint IDs are device names, not addresses.** Renaming a device is seen as
one removed and one added, which is correct — its metrics change identity
anyway. Using the IP would be wrong: two devices can briefly share one during
a renumber, and the watcher would treat them as the same endpoint.

**Devices without a name or a `primary_ip4` are skipped.** There is nothing to
point a receiver at.

**Paging is bounded at 20 pages of 200 per status.** A filter that fails to
narrow anything would otherwise walk the entire database.

## Testing

```sh
go test -race ./...
```
