# HTTPS certificates

A machine can get a TLS certificate for its MagicDNS name with
`tailscale cert` or `tailscale serve`, the way it does on Tailscale. The
client asks Let's Encrypt for the certificate and proves it holds the name
with a DNS-01 challenge: a TXT record under `_acme-challenge.<name>`. It
cannot publish that record itself, so it sends it to the control server
over `/machine/set-dns`, and Slopscale publishes it in the zone that holds
the tailnet's names.

This needs a base domain that is a real zone on the public internet, and
a way for Slopscale to write to it.

## Setting it up

```yaml
dns:
  base_domain: ts.example.com

https_certificates:
  enabled: true
  provider: cloudflare
  cloudflare:
    api_token: ${CLOUDFLARE_API_TOKEN}
```

With `enabled`, every machine's map response lists its MagicDNS name,
such as `laptop.ts.example.com`, as a cert domain, and the client picks
DNS-01 when it needs a certificate. The names only need to exist in
public DNS for the challenge record; they do not need to resolve to
anything, and the machines stay reachable over the tailnet alone.

Three providers publish the record:

- `cloudflare` uses the Cloudflare API with `api_token`, a token holding
  _Zone: Read_ and _DNS: Edit_ on the zone. The zone is found from the
  record's name, or pinned with `zone_id`.
- `rfc2136` sends a dynamic DNS update to `server` (host and port) for
  `zone`, which defaults to the base domain, signed with `tsig_key_name`,
  `tsig_secret` and `tsig_algorithm` (`hmac-sha256` by default) when a key
  is set. BIND, Knot and PowerDNS take these.
- `command` runs the program at `path` with the record name and value
  as its two arguments, for any other zone. A non-zero exit fails the
  challenge with the program's output.

`ttl` is the records' time to live, a minute by default.

## What a machine may publish

A machine may publish only the challenge record of its own MagicDNS name,
`_acme-challenge.<its name>.<base domain>`, and only a TXT record, over
its own Noise session. Anything else is refused. Every record published
lands in the [audit log](audit.md) as `node.cert_challenge` on the
machine.

Slopscale does not delete challenge records afterwards; they are
harmless, and Let's Encrypt asks for a fresh value each time. The
`cloudflare` provider skips a record whose value is already there, and
each provider keeps the other values at the name, because a certificate
for a name and its wildcard needs two challenges at once.

## Without a public zone

A base domain that only exists inside the tailnet cannot pass a DNS-01
challenge, because Let's Encrypt looks the record up from the outside.
Machines can still use `tailscale cert` with certificates issued some
other way, or serve plain HTTP; nothing else changes.
