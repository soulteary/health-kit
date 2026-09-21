# Security Policy

## Reporting a vulnerability

**Please do not open a public issue for a security problem.**

Report it through GitHub's private vulnerability reporting: go to the
[Security tab](https://github.com/soulteary/health-kit/security) and choose
**Report a vulnerability**. That opens a private advisory visible only to the
maintainers.

If you do not see that option, open a normal issue saying only that you have a
security report and need a private channel — **no details, no reproducer** —
and a maintainer will arrange one.

Please include, once you have a private channel:

- the affected version or commit,
- what an attacker can do, and what they need in order to do it,
- a reproducer, if you have one.

Expect an acknowledgement within a few days. This is a small
volunteer-maintained project, so please allow reasonable time for a fix before
disclosing publicly.

## Supported versions

| Version | Supported |
| ------- | --------- |
| 4.x     | ✅ |
| 3.x     | ❌ superseded by 4.x — see the [migration notes](README.md#installation) |
| 2.x     | ❌ |
| 1.x     | ❌ |

Fixes land on the latest minor of the current major. There are no long-term
support branches.

## What this library does, and what it does not

health-kit serves a health endpoint. Its security-relevant behaviour is
concentrated in two places, and both depend on how *you* configure it.

### Health endpoints are usually unauthenticated

This library does not authenticate anyone. `Config.IPWhitelist` is the only
access control it provides, and it is off by default. If your `/healthz` is
reachable from the internet, assume the response is public.

### Detail is opt-in, because probe errors leak infrastructure

`DefaultConfig()` reports only an overall status and the service name. That is
deliberate: the built-in probes put `err.Error()` into the result verbatim,
which for a database or Redis probe means DSNs, internal hostnames and
filesystem paths.

`DefaultInternalConfig()` turns that detail on. **An aggregator built from it
should not be exposed publicly** — put it behind an IP whitelist, network
policy, or an authenticating proxy.

```go
// Public endpoint.
health.DefaultConfig().WithServiceName("myservice")

// Internal endpoint — detail, restricted to the cluster.
health.DefaultInternalConfig().
    WithServiceName("myservice").
    WithIPWhitelist([]string{"10.0.0.0/8"})
```

### Forwarded headers are believed only from a trusted proxy

`X-Forwarded-For` and `X-Real-IP` are attacker-controlled: anything that can
reach your endpoint can set them. They are honoured **only** when the peer
address is covered by `Config.TrustedProxies`, which is empty by default.

This matters when `IPWhitelist` is in use. Without `TrustedProxies`, the
whitelist is applied to the real peer address. With it, the whitelist is
applied to the forwarded client — so `TrustedProxies` must list your proxies
and nothing else. Setting it to `0.0.0.0/0` means anyone can spoof their way
past the whitelist.

The rule resolves the **left-most** entry of `X-Forwarded-For`, and if that
entry is not a valid IP it does **not** fall through to the next hop — a
forged first entry cannot be used to pick a whitelisted address further down
the chain.

There is exactly one implementation of this rule, `Config.ClientIP`, shared by
every adapter. If you write an adapter for another framework, implement
`ClientIPSource` and call it; do not reimplement the rule. A trusted-proxy rule
that disagrees with itself across frameworks is a whitelist bypass. See the
adapter section in the README.

### Probes reach out to your dependencies

Each check opens a connection to whatever you configured — Redis, an HTTP URL,
a database. An unauthenticated health endpoint is therefore a way to make your
service issue those requests on demand. `Config.Timeout` bounds how long that
takes, but it does not bound how often; rate limiting, if you need it, belongs
in front of the endpoint.
