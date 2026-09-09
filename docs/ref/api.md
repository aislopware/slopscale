# API

Slopscale provides a [HTTP REST API](#rest-api) which drives the built-in [admin console](console.md), may be used to
[remote control Slopscale](#remote-control) or provide a base for custom
integration and tooling.

The API requires a valid API key before use. To create an API key, log into your Slopscale server and generate
one with the default expiration of 90 days:

```shell
slopscale apikeys create
```

Copy the output of the command and save it for later. An API key cannot be retrieved again. If the API
key is lost, expire the old one, and create a new one.

A key created this way is all-access. To hand out less, create the key for a user, so it is bounded by the user's
[role](roles.md):

```shell
slopscale apikeys create --user <USER_ID>
```

A key can also be limited to some operations with scopes, the same vocabulary the OAuth clients use, and carry a
description saying what it is for. The scopes never reach past the caller that mints the key or the role of the
user that owns it: a scope the caller cannot delegate is dropped, and a caller that can delegate none of them is
refused. A key without scopes keeps its owner's whole role, unless the caller is itself a scoped key, in which
case the new key inherits the caller's scopes. An OAuth access token cannot mint API keys at all: a token is
bounded by tags as well as scopes, and an API key has no tags to be bounded by.

```shell
slopscale apikeys create --user <USER_ID> --scope dns --scope devices:core:read --description "Resolver sync"
```

The console's _Keys_ page offers the same when creating an API key, and lists each key's scopes and description.

To list the API keys currently associated with the server:

```shell
slopscale apikeys list
```

and to expire an API key:

```shell
slopscale apikeys expire --prefix <PREFIX>
```

A key can be rotated instead of replaced. Rotating mints a new secret for the
same key and prints it once; the key keeps its id, owner, scopes, description
and expiry, and the old secret stops working immediately:

```shell
slopscale apikeys rotate --prefix <PREFIX>
```

Pass `--expiration` to give the rotated key a new expiry; without it the key
keeps the one it has. An expired key cannot be rotated, because expiring a key
is how it is revoked, so create a new key instead. A key that carries its own
scopes may only rotate a key whose scopes it could have minted itself, so
rotation never widens a credential. The console's _Keys_ page offers the same
action, and every rotation is recorded in the audit log as `apikey.rotate`,
naming the prefix the key had and the one it now answers to.

## REST API

- API endpoint: `/api/v1`, e.g. `https://slopscale.example.com/api/v1`
- Documentation: `/api/v1/docs`, e.g. `https://slopscale.example.com/api/v1/docs`
- Slopscale Version: `/version`, e.g. `https://slopscale.example.com/version`
- Authenticate using HTTP Bearer authentication by sending the [API key](#api) with the HTTP `Authorization: Bearer <API_KEY>` header.

Start by [creating an API key](#api) and test it with the examples below. Read the API documentation provided by your
Slopscale server at `/api/v1/docs` for details.

=== "Get details for all users"

    ```console
    curl -H "Authorization: Bearer <API_KEY>" \
        https://slopscale.example.com/api/v1/user
    ```

=== "Get details for user 'bob'"

    ```console
    curl -H "Authorization: Bearer <API_KEY>" \
        https://slopscale.example.com/api/v1/user?name=bob
    ```

=== "Register a node"

    ```console
    curl -H "Authorization: Bearer <API_KEY>" \
        --json '{"user": "<USER>", "authId": "<AUTH_ID>"}' \
        https://slopscale.example.com/api/v1/auth/register
    ```

## Remote control

The `slopscale` binary can control a Slopscale instance from a remote machine over the HTTP API.

### Prerequisite

- A workstation to run `slopscale` (any supported platform, e.g. Linux).
- The Slopscale server reachable over HTTP(S).
- An [API key](#api) to authenticate with the Slopscale server.

### Setup remote control

1. Download the [`slopscale` binary from GitHub's release page](https://github.com/aislopware/slopscale/releases). Make
   sure to use the same version as on the server.

1. Put the binary somewhere in your `PATH`, e.g. `/usr/local/bin/slopscale`

1. Make `slopscale` executable: `chmod +x /usr/local/bin/slopscale`

1. [Create an API key](#api) on the Slopscale server.

1. Provide the connection parameters for the remote Slopscale server either via a minimal YAML configuration file or
   via environment variables:

    === "Minimal YAML configuration file"

        ```yaml title="config.yaml"
        cli:
            address: <SLOPSCALE_URL>
            api_key: <API_KEY>
        ```

    === "Environment variables"

        ```shell
        export SLOPSCALE_CLI_ADDRESS="<SLOPSCALE_URL>"
        export SLOPSCALE_CLI_API_KEY="<API_KEY>"
        ```

    This instructs the `slopscale` binary to connect to a remote instance at `<SLOPSCALE_URL>` (e.g.
    `https://slopscale.example.com`), instead of connecting to the local instance. A bare host without a scheme is
    assumed to be `https`.

1. Test the connection by listing all nodes:

    ```shell
    slopscale nodes list
    ```

    You should now be able to see a list of your nodes from your workstation, and you can
    now control the Slopscale server from your workstation.

### Behind a proxy

The remote CLI uses the same HTTP API as everything else, so it works through the reverse proxy already in front of
Slopscale with no extra setup.

### Troubleshooting

- Make sure you have the _same_ Slopscale version on your server and workstation.
- Verify that your TLS certificate is valid and trusted.
- If you don't have access to a trusted certificate (e.g. from Let's Encrypt), either:
    - Add your self-signed certificate to the trust store of your OS _or_
    - Disable certificate verification by either setting `cli.insecure: true` in the configuration file or by setting
      `SLOPSCALE_CLI_INSECURE=1` via an environment variable. We do **not** recommend to disable certificate validation.
