# Draft issue for opentelemetry-collector-contrib

Template: **Feature request**
Suggested title:

> [receiver/receivercreator] Endpoint types are validated against a closed list, blocking observers outside this repository

Labels applied automatically: `enhancement`, `needs triage`
Component: `receiver/receivercreator`

---

## Component(s)

receiver/receivercreator, extension/observer

## Is your feature request related to a problem? Please describe.

`observer.EndpointDetails` is an open interface:

```go
// EndpointDetails provides additional context about an endpoint such as a Pod or Port.
type EndpointDetails interface {
	Env() EndpointEnv
	Type() EndpointType
}
```

Any extension can implement it and emit endpoints with an `EndpointType` of its
own choosing. `EndpointType` is a plain string type, and nothing in the observer
package restricts its values.

`receiver_creator`, however, validates endpoint types against a closed list of
the eight built-in types, in two independent places.

**1. Rule parsing** — [`receiver/receivercreator/rules.go`][rules]:

```go
var ruleRe = regexp.MustCompile(
	fmt.Sprintf(`^type\s*==\s*(%q|%q|%q|%q|%q|%q|%q|%q)`,
		observer.PodType, observer.K8sServiceType, observer.K8sIngressType,
		observer.PortType, observer.PodContainerType, observer.HostPortType,
		observer.ContainerType, observer.K8sNodeType),
)
```

A rule naming any other type fails to parse.

**2. Resource attribute validation** — [`receiver/receivercreator/config.go`][config],
in `Unmarshal`:

```go
for endpointType := range cfg.ResourceAttributes {
	switch endpointType {
	case observer.ContainerType, observer.K8sServiceType, observer.K8sIngressType,
		observer.HostPortType, observer.K8sNodeType, observer.PodType,
		observer.PortType, observer.PodContainerType:
	default:
		return fmt.Errorf("resource attributes for unsupported endpoint type %q", endpointType)
	}
}
```

The result is that an observer maintained outside this repository can produce a
perfectly valid `EndpointDetails` implementation that `receiver_creator` then
refuses to consume. The extension point exists at the interface, and is closed
at the only consumer.

[rules]: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/receivercreator/rules.go
[config]: https://github.com/open-telemetry/opentelemetry-collector-contrib/blob/main/receiver/receivercreator/config.go

## Describe the solution you'd like

Every observer in this repository today — `k8sobserver`, `dockerobserver`,
`ecsobserver`, `ecstaskobserver`, `hostobserver` — discovers workloads on a
container scheduler or the local host, so the eight built-in types cover every
in-tree case and the limitation has not been felt.

It is felt immediately outside that set. As a concrete example, we maintain an
observer that discovers physical network devices from a source-of-truth
database (NetBox) and emits one endpoint per device, so that SNMP and
vendor-API receivers are created and torn down as devices are commissioned,
fail, or are decommissioned — the same lifecycle `receiver_creator` already
provides for pods. The natural endpoint type for that is something like
`"device"`. Because neither location accepts it, the observer instead emits
`observer.PortType`, which carries `port` semantics the endpoint does not have
and makes the resulting rules misleading to read.

A few possible directions, in rough order of how invasive they are — we have no
strong preference and would defer to maintainers:

1. **Relax rule parsing to accept any syntactically valid type**, e.g.
   `^type\s*==\s*"[a-z][a-z0-9._-]*"`, leaving it to the expression evaluation
   to match or not match. This is the smallest change and keeps the "rule must
   start with a type check" guarantee that `ruleRe` exists to enforce.
2. **Allow resource attributes for unknown types** rather than erroring, so
   custom endpoint types can carry attributes like the built-in ones do.
3. **A registration mechanism**, where observers declare the endpoint types
   they emit and `receiver_creator` validates against the union of registered
   types. More machinery, but it preserves validation of genuine typos, which
   options 1 and 2 give up.

Options 1 and 2 are independent and could land separately.

## Describe alternatives you've considered

- **Reusing a built-in type** (what we do today). It works, but the endpoint
  type no longer describes the endpoint, and any future semantics attached to
  `PortType` could collide.
- **Forking `receiver_creator`.** Avoidable duplication of a non-trivial and
  actively maintained component.
- **Generating static configuration instead.** This is the toil
  `receiver_creator` exists to remove, and it costs a collector restart on
  every inventory change.

## Additional context

Happy to open a PR for whichever direction maintainers prefer.

---

## Notes

- Opening an issue requires no CLA. A pull request does, so if the outcome is
  "send a PR", sign the Linux Foundation CLA at that point — the bot prompts
  from the PR itself.
- Worth raising in `#otel-collector` on CNCF Slack once filed. Component
  issues move faster when a maintainer is already aware of them.
