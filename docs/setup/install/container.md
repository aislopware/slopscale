# Running slopscale in a container

!!! warning "Community documentation"

    This page is not actively maintained by the slopscale authors and is
    written by community members. It is _not_ verified by slopscale developers.

    **It might be outdated and it might miss necessary steps**.

A container runtime such as [Docker](https://www.docker.com) or [Podman](https://podman.io) is required. The container
image is on the [GitHub Container Registry](https://github.com/aislopware/slopscale/pkgs/container/slopscale) as
`ghcr.io/aislopware/slopscale:<VERSION>`.

## Configure and run slopscale

1. Create a directory on the container host to store slopscale's [configuration](../../ref/configuration.md) and the SQLite database:

    ```shell
    mkdir -p ./slopscale/{config,lib}
    cd ./slopscale
    ```

1. Download the example configuration for your chosen version and save it as: `$(pwd)/config/config.yaml`. Adjust the
   configuration to suit your local environment. See [Configuration](../../ref/configuration.md) for details.

1. Start slopscale from within the previously created `./slopscale` directory:

    ```shell
    docker run \
      --name slopscale \
      --detach \
      --read-only \
      --tmpfs /var/run/slopscale \
      --volume "$(pwd)/config:/etc/slopscale:ro" \
      --volume "$(pwd)/lib:/var/lib/slopscale" \
      --publish 127.0.0.1:8080:8080 \
      --publish 127.0.0.1:9090:9090 \
      --health-cmd "CMD slopscale health" \
      ghcr.io/aislopware/slopscale:<VERSION> \
      serve
    ```

    Note: use `0.0.0.0:8080:8080` instead of `127.0.0.1:8080:8080` if you want to expose the container externally.

    This command mounts the local directories inside the container, forwards port 8080 and 9090 out of the container so
    the slopscale instance becomes available and then detaches so slopscale runs in the background.

    A similar configuration for `docker-compose`:

    ```yaml title="docker-compose.yaml"
    services:
      slopscale:
        image: ghcr.io/aislopware/slopscale:<VERSION>
        restart: unless-stopped
        container_name: slopscale
        read_only: true
        tmpfs:
          - /var/run/slopscale
        ports:
          - "127.0.0.1:8080:8080"
          - "127.0.0.1:9090:9090"
        volumes:
          # Please set <SLOPSCALE_PATH> to the absolute path
          # of the previously created slopscale directory.
          - <SLOPSCALE_PATH>/config:/etc/slopscale:ro
          - <SLOPSCALE_PATH>/lib:/var/lib/slopscale
        command: serve
        healthcheck:
            test: ["CMD", "slopscale", "health"]
    ```

1. Verify slopscale is running:

    Follow the container logs:

    ```shell
    docker logs --follow slopscale
    ```

    Verify running containers:

    ```shell
    docker ps
    ```

    Verify slopscale is available:

    ```shell
    curl http://127.0.0.1:8080/health
    ```

Continue on the [getting started page](../../usage/getting-started.md) to register your first machine.

## Debugging slopscale running in Docker

The Slopscale container image is based on a distroless image that does not contain a shell or any other debug tools. If you need to debug slopscale running in the Docker container, you can use the `-debug` variant, for example `ghcr.io/aislopware/slopscale:x.x.x-debug`.

### Running the debug Docker container

To run the debug Docker container, use the exact same commands as above, but replace `ghcr.io/aislopware/slopscale:x.x.x` with `ghcr.io/aislopware/slopscale:x.x.x-debug` (`x.x.x` is the version of slopscale). The two containers are compatible with each other, so you can alternate between them.

### Executing commands in the debug container

The default command in the debug container is to run `slopscale`, which is located at `/ko-app/slopscale` inside the container.

Additionally, the debug container includes a minimalist Busybox shell.

To launch a shell in the container, use:

```shell
docker run -it ghcr.io/aislopware/slopscale:x.x.x-debug sh
```

You can also execute commands directly, such as `ls /ko-app` in this example:

```shell
docker run ghcr.io/aislopware/slopscale:x.x.x-debug ls /ko-app
```

Using `docker exec -it` allows you to run commands in an existing container.
