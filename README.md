<h1 align="center">
  <img src="docs/assets/logo/slopscale-mark.svg" alt="" width="56"><br>
  slopscale
</h1>

<p align="center">An open source, self-hosted implementation of the Tailscale control server.</p>

<p align="center">
  <a href="https://aislopware.github.io/slopscale/">Documentation</a>
  &nbsp;·&nbsp;
  <a href="https://aislopware.github.io/slopscale/usage/getting-started/">Getting started</a>
  &nbsp;·&nbsp;
  <a href="https://aislopware.github.io/slopscale/setup/install/container/">Install</a>
  &nbsp;·&nbsp;
  <a href="CHANGELOG.md">Changelog</a>
</p>

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="docs/assets/readme/console-dark.png">
    <img alt="The slopscale admin console, open on one machine" src="docs/assets/readme/console-light.png">
  </picture>
</p>

Point the stock Tailscale clients at slopscale instead of the hosted control
plane: your machines exchange WireGuard keys, get their IP addresses, DNS
and DERP relay from a server you run, under an access policy you write.

slopscale is a fork of [headscale](https://github.com/juanfont/headscale)
with these improvements:

- Faster map responses, lower memory use, a smaller database footprint
- Built-in admin console at `/console/`, signed in through your identity
  provider, with a terminal to any machine running Tailscale SSH
- User roles, device and user approval, invitations, machine sharing, groups
  and access rules, networks, temporary access, device posture and postures,
  hardware attestation, suspension
- Tailscale features the hosted control plane has: Services, app connectors,
  Funnel, HTTPS certificates for Serve, tailnet lock, identity tokens, SSH
  session recording, key expiry, client update notices
- OAuth clients with scopes and workload identity federation, so the Tailscale
  Terraform provider and Kubernetes operator work unchanged
- DNS, DERP relays, key expiry and certificates configurable at runtime, with
  an embedded relay on by default
- Webhooks and chat or email notifications, log streaming to a SIEM, audit
  log, client updates and diagnostics over the control connection
- Bug fixes to the control protocol: exit node suggestions, ephemeral
  registration, DNS records, deleted machines, and more

The [documentation](https://aislopware.github.io/slopscale/) covers setup and
every feature. [CHANGELOG.md](CHANGELOG.md) lists what changed against
headscale, including the rename.

This project is not associated with Tailscale Inc. or the headscale project.
