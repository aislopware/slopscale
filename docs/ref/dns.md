# DNS

Slopscale supports [most DNS features](../about/features.md) from Tailscale. DNS related settings can be configured
within the `dns` section of the [configuration file](configuration.md), and most of them can be changed while the
server runs, from the admin console, the API or the CLI.

## Changing DNS settings at runtime

The `dns` section of the configuration file is what a fresh server sends to its clients. Everything in it except
`magic_dns` and `base_domain` can be replaced without a restart:

- the global nameservers (`nameservers.global`) and whether clients use them for every query
  (`override_local_dns`),
- split DNS (`nameservers.split`), the resolvers for particular domains,
- the search domains (`search_domains`), and
- the extra records (`extra_records`).

MagicDNS and the base domain name the machines, so they stay in the configuration file.

The admin console edits the settings under _DNS_. The API has `GET`, `PUT` and `DELETE /api/v1/dns`, where `PUT`
replaces every setting at once and `DELETE` returns to the configuration file, and the v2 API has Tailscale's
`/api/v2/tailnet/-/dns/nameservers`, `/dns/preferences`, `/dns/searchpaths` and `/dns/split-dns` endpoints, so the
Tailscale Terraform provider and other tooling written against them work unchanged. Both need the `dns` scope
(`dns:read` to look); a network admin holds it, an IT admin can only read. The CLI mirrors the API:

```console
$ slopscale dns show
$ slopscale dns set --nameserver 1.1.1.1 --nameserver 1.0.0.1 --override-local-dns \
    --split corp.example=10.0.0.53 --search-domain corp.example \
    --record grafana.myvpn.example.com=100.64.0.3
$ slopscale dns reset
```

Settings set this way are stored in the database and survive restarts. While they are in force the configuration
file's `dns` section is ignored, except for `magic_dns`, `base_domain` and `extra_records_path`; `slopscale dns show`
and `GET /api/v1/dns` report both what clients receive and what the file says, and a reset goes back to the file. Every
change is pushed to the clients at once and logged in the [audit log](audit.md) as `dns.set` or `dns.reset`.

When `dns.extra_records_path` is set, that file owns the extra records: the records set at runtime are rejected and the
console shows the file's records read-only. The other settings can still be edited; the file's records stay in force.

Only what the Tailscale client can use is accepted. A nameserver is an IP address, an IP with a port, or the DNS over
HTTPS URL of a provider the client knows how to reach without bootstrap DNS (Cloudflare, Google, Quad9, NextDNS,
ControlD and the other entries of Tailscale's `publicdns` list); the client does not do DNS over TLS or DNS over HTTPS
to an arbitrary host, so `tls://` URLs and unknown `https://` URLs are refused. Extra records are `A` or `AAAA`; the
client serves nothing else.

## Keeping nameservers while an exit node is in use

A machine that routes through an exit node sends its DNS through the exit node as well, and drops the tailnet's
nameservers. Tailscale's admin console has a per-nameserver "Use with exit node" setting that keeps a nameserver in
use during that time; slopscale has the same, since clients from Tailscale 1.88.1 honour it:

- in the configuration file, `dns.nameservers.use_with_exit_node.global` lists the global nameservers to keep and
  `dns.nameservers.use_with_exit_node.split` the split DNS nameservers per domain,
- on the console's _DNS_ page, the switch next to each nameserver and each split DNS domain,
- `slopscale dns set --use-with-exit-node 1.1.1.1 --split-use-with-exit-node corp.example=10.0.0.53`, and
- `useWithExitNode` and `splitUseWithExitNode` in `PUT /api/v1/dns`.

Two client rules shape what is accepted. The client honours the flag only on the resolvers it uses for every query,
so a global nameserver can be kept only while `override_local_dns` is on; turning the override off drops the marks.
A split DNS domain survives the exit node only when every one of its nameservers is kept, so the console marks the
whole domain at once. A nameserver must be one of the configured ones; removing it, or replacing the nameservers
through the v2 API, drops its mark.

## Split DNS per group

The split DNS above reaches every machine. A group DNS rule hands domains and nameservers to the machines of some
[groups](access-control.md) only: a rule names the zones, the resolvers that answer for them and the groups that
receive it, and the machines in those groups send queries for the zones there, on top of the split DNS everyone
gets. A machine joining or leaving a group picks the rule up or loses it at once. A domain the tailnet also splits
keeps the global resolvers first and adds the rule's after them. The nameservers take the same forms as everywhere
else, and a group a rule names cannot be deleted until the rule drops it. The zones MagicDNS answers on the client,
the base domain and the reverse zones of the tailnet's prefixes, cannot be routed by a rule.

Rules live on the console's _DNS_ page under _Split DNS per group_, in `slopscale dns rules` (`list`, `create`,
`update`, `delete`) and at `/api/v1/dns/rule` (`GET`, `POST`, `PUT /{id}`, `DELETE /{id}`), gated by the `dns` and
`dns:read` scopes and logged as `dns.rule.create`, `dns.rule.update` and `dns.rule.delete`:

```console
$ slopscale dns rules create --name "Corp DNS" --domain corp.example.com --nameserver 10.0.0.53 --group 2
```

## Setting extra DNS records

Slopscale allows to set extra DNS records which are made available via
[MagicDNS](https://tailscale.com/docs/features/magicdns). Extra DNS records can be configured either via static entries
in the [configuration file](configuration.md) or from a JSON file that Slopscale continuously watches for changes:

- Use the `dns.extra_records` option in the [configuration file](configuration.md) for entries that are static and
  don't change while Slopscale is running. Those entries are processed when Slopscale is starting up and changes to the
  configuration require a restart of Slopscale.
- For dynamic DNS records that may be added, updated or removed while Slopscale is running or DNS records that are
  generated by scripts the option `dns.extra_records_path` in the [configuration file](configuration.md) is useful.
  Set it to the absolute path of the JSON file containing DNS records and Slopscale processes this file as it detects
  changes.

An example use case is to serve multiple apps on the same host via a reverse proxy like NGINX, in this case a Prometheus
monitoring stack. This allows to nicely access the service with "http://grafana.myvpn.example.com" instead of the
hostname and port combination "http://hostname-in-magic-dns.myvpn.example.com:3000".

!!! warning "Limitations"

    Currently, [only A and AAAA records are processed by Tailscale](https://github.com/tailscale/tailscale/blob/v1.86.5/ipn/ipnlocal/node_backend.go#L662).

1. Configure extra DNS records using one of the available configuration options:

    === "Static entries, via `dns.extra_records`"

        ```yaml title="config.yaml"
        dns:
          ...
          extra_records:
            - name: "grafana.myvpn.example.com"
              type: "A"
              value: "100.64.0.3"

            - name: "prometheus.myvpn.example.com"
              type: "A"
              value: "100.64.0.3"
          ...
        ```

        Restart your slopscale instance.

    === "Dynamic entries, via `dns.extra_records_path`"

        ```json title="extra-records.json"
        [
          {
            "name": "grafana.myvpn.example.com",
            "type": "A",
            "value": "100.64.0.3"
          },
          {
            "name": "prometheus.myvpn.example.com",
            "type": "A",
            "value": "100.64.0.3"
          }
        ]
        ```

        Slopscale picks up changes to the above JSON file automatically.

        !!! tip "Good to know"

            - The `dns.extra_records_path` option in the [configuration file](configuration.md) needs to reference the
              JSON file containing extra DNS records.
            - Be sure to "sort keys" and produce a stable output in case you generate the JSON file with a script.
              Slopscale uses a checksum to detect changes to the file and a stable output avoids unnecessary processing.
            - Slopscale reads the file once it has been left alone for a moment, so a script may write it in several
              steps. Record names are lowercased on the way in, as a client only matches lowercase names.

1. Verify that DNS records are properly set using the DNS querying tool of your choice:

    === "Query with dig"

        ```console
        dig +short grafana.myvpn.example.com
        100.64.0.3
        ```

    === "Query with drill"

        ```console
        drill -Q grafana.myvpn.example.com
        100.64.0.3
        ```

1. Optional: Setup the reverse proxy

    The motivating example here was to be able to access internal monitoring services on the same host without
    specifying a port, depicted as NGINX configuration snippet:

    ```nginx title="nginx.conf"
    server {
        listen 80;
        listen [::]:80;

        server_name grafana.myvpn.example.com;

        location / {
            proxy_pass http://localhost:3000;
            proxy_set_header Host $http_host;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
        }

    }
    ```
