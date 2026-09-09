# Configuration

- Slopscale loads its configuration from a YAML file
- It searches for `config.yaml` in the following paths:
    - `/etc/slopscale`
    - `$HOME/.slopscale`
    - the current working directory
- To load the configuration from a different path, use:
    - the command line flag `-c`, `--config`
    - the environment variable `SLOPSCALE_CONFIG`
- Validate the configuration file with: `slopscale configtest`

!!! example "Get the [example configuration from the GitHub repository](https://github.com/aislopware/slopscale/blob/main/config-example.yaml)"

    Always select the [same GitHub tag](https://github.com/aislopware/slopscale/tags) as the released version you use to
    ensure you have the correct example configuration. The `main` branch might contain unreleased changes.

    === "View on GitHub"

        - Development version: <https://github.com/aislopware/slopscale/blob/main/config-example.yaml>
        - Version {{ slopscale.version }}: https://github.com/aislopware/slopscale/blob/v{{ slopscale.version }}/config-example.yaml

    === "Download with `wget`"

        ```shell
        # Development version
        wget -O config.yaml https://raw.githubusercontent.com/aislopware/slopscale/main/config-example.yaml

        # Version {{ slopscale.version }}
        wget -O config.yaml https://raw.githubusercontent.com/aislopware/slopscale/v{{ slopscale.version }}/config-example.yaml
        ```

    === "Download with `curl`"

        ```shell
        # Development version
        curl -o config.yaml https://raw.githubusercontent.com/aislopware/slopscale/main/config-example.yaml

        # Version {{ slopscale.version }}
        curl -o config.yaml https://raw.githubusercontent.com/aislopware/slopscale/v{{ slopscale.version }}/config-example.yaml
        ```
