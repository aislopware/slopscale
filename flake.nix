{
  description = "slopscale - Open Source Tailscale Control server";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    # Reusable Go flake checks (build/test/lint/format); CI runs them via
    # `nix build .#checks.<system>.<name>` instead of bespoke per-tool steps.
    flake-checks.url = "github:kradalby/flake-checks";
    flake-checks.inputs.nixpkgs.follows = "nixpkgs";
  };

  outputs =
    { self
    , nixpkgs
    , flake-utils
    , flake-checks
    , ...
    }:
    let
      slopscaleVersion = self.shortRev or self.dirtyShortRev;
      commitHash = self.rev or self.dirtyRev;
      # C flags for the SQLite bundled in mattn/go-sqlite3 (see
      # sqlite.cflags); exported as CGO_CFLAGS wherever Go compiles it.
      sqliteCFlags = nixpkgs.lib.concatStringsSep " "
        (nixpkgs.lib.filter (l: l != "" && !nixpkgs.lib.hasPrefix "#" l)
          (nixpkgs.lib.splitString "\n" (builtins.readFile ./sqlite.cflags)));
    in
    {
      # NixOS module
      nixosModules = rec {
        slopscale = import ./nix/module.nix;
        default = slopscale;
      };

      overlays.default = _: prev:
        let
          pkgs = nixpkgs.legacyPackages.${prev.stdenv.hostPlatform.system};
          # Tracks the newest Go in nixpkgs (currently 1.27) so a Go release
          # bump is a flake.lock update, not a flake.nix edit.
          buildGo = pkgs.buildGoLatestModule;
          vendorHash = (builtins.fromJSON (builtins.readFile ./flakehashes.json)).vendor.sri;
        in
        {
          slopscale = buildGo {
            pname = "slopscale";
            version = slopscaleVersion;
            src = pkgs.lib.cleanSource self;

            # Only run unit tests when testing a build
            checkFlags = [ "-short" ];

            # vendorHash is read from flakehashes.json; refresh via:
            #   go run ./cmd/vendorhash update
            inherit vendorHash;

            # The SQLite driver is C compiled by cgo (buildGoModule enables
            # cgo whenever a C compiler is present).
            env.CGO_CFLAGS = sqliteCFlags;

            subPackages = [ "cmd/slopscale" ];

            meta = {
              mainProgram = "slopscale";
            };
          };

          hi = buildGo {
            pname = "hi";
            version = slopscaleVersion;
            src = pkgs.lib.cleanSource self;

            checkFlags = [ "-short" ];
            inherit vendorHash;

            env.CGO_CFLAGS = sqliteCFlags;

            subPackages = [ "cmd/hi" ];
          };

          gotestsum = prev.gotestsum.override {
            buildGoModule = buildGo;
          };

          gotests = prev.gotests.override {
            buildGoModule = buildGo;
          };

          gofumpt = prev.gofumpt.override {
            buildGoModule = buildGo;
          };

          golines = prev.golines.override {
            buildGoModule = buildGo;
          };

          # goimports and friends: they parse Go with the parser of the Go
          # they were built with, so an older one rejects new syntax.
          gotools = prev.gotools.override {
            buildGoModule = buildGo;
            # goimports is wrapped with this go on PATH for module lookups.
            go = pkgs.go_latest;
          };

          golangci-lint-langserver = prev.golangci-lint-langserver.override {
            buildGoModule = buildGo;
          };

          # web/bun.lock is written by bun 1.4 (lockfile version 2), which
          # older bun cannot parse; pin the version the console is built with.
          bun = prev.bun.overrideAttrs (finalAttrs: _: {
            version = "1.4.1";
            __intentionallyOverridingVersion = true;
            passthru = {
              sources = {
                "aarch64-darwin" = prev.fetchurl {
                  url = "https://github.com/oven-sh/bun/releases/download/bun-v${finalAttrs.version}/bun-darwin-aarch64.zip";
                  hash = "sha256-2Jc86DX6eGflzHmv7m/G8a4BF6pL1fwlRv0AxRL3E4Y=";
                };
                "aarch64-linux" = prev.fetchurl {
                  url = "https://github.com/oven-sh/bun/releases/download/bun-v${finalAttrs.version}/bun-linux-aarch64.zip";
                  hash = "sha256-WAzndTMQjcaxC+wXITl+T1qkTpCXJtokUdSD38XlgdY=";
                };
                "x86_64-linux" = prev.fetchurl {
                  url = "https://github.com/oven-sh/bun/releases/download/bun-v${finalAttrs.version}/bun-linux-x64-baseline.zip";
                  hash = "sha256-qMnGc4IC4vztVV3YYKlTxWwM0Fn3UEHnAQroGjKAJkY=";
                };
              };
            };
          });
        };
    }
    // flake-utils.lib.eachDefaultSystem
      (system:
      let
        pkgs = import nixpkgs {
          overlays = [ self.overlays.default ];
          inherit system;
        };
        buildDeps = with pkgs; [ git go_latest gnumake ];
        # zig is the C cross compiler behind zigcc for the Linux release
        # binaries and container images (cgo needs a compiler per target).
        crossDeps = with pkgs; [ zig ];
        devDeps = with pkgs;
          buildDeps
          ++ crossDeps
          ++ [
            golangci-lint
            golangci-lint-langserver
            # Admin console toolchain (web/): bun runs the pinned oxlint,
            # oxfmt, TypeScript and Vite from web/bun.lock.
            bun
            golines
            nixpkgs-fmt
            goreleaser
            nfpm
            gotestsum
            gotests
            gofumpt
            gopls
            gotools

            ksh
            ko
            yq-go
            ripgrep
            postgresql

            # External clients exercised by the Tailscale-compatible v2 API
            # roundtrip tests (TestAPIv2). Binaries: tofu, tscli.
            opentofu
            tscli
            prek

            # 'dot' is needed for pprof graphs
            # go tool pprof -http=: <source>
            graphviz
          ]
          ++ lib.optionals pkgs.stdenv.hostPlatform.isLinux [ traceroute ];

        # Add entry to build a docker image with slopscale
        # caveat: only works on Linux
        #
        # Usage:
        # nix build .#slopscale-docker
        # docker load < result
        slopscale-docker = pkgs.dockerTools.buildLayeredImage {
          name = "slopscale";
          tag = slopscaleVersion;
          contents = [ pkgs.slopscale ];
          config.Entrypoint = [ (pkgs.slopscale + "/bin/slopscale") ];
        };

        # Go flake checks from the flake-checks library. CI gates on
        # `nix build .#checks.<system>.<name>`; the logic lives here, not in
        # bespoke workflow steps. Linux-only: parts of the tree are
        # Linux-specific and the pure unit subset is validated by CI.
        fc = flake-checks.lib;
        common = {
          inherit pkgs;
          root = ./.;
          pname = "slopscale";
          version = slopscaleVersion;
          vendorHash = (builtins.fromJSON (builtins.readFile ./flakehashes.json)).vendor.sri;
          goPkg = pkgs.go_latest;
          # //go:embed targets and test-read files outside the default whitelist.
          embedDirs = [
            ./hscontrol/assets
            ./hscontrol/db/schema.sql
            ./hscontrol/db/schema_postgres.sql
            ./config-example.yaml
            # The console embed needs its directory even when only .gitkeep is in it.
            ./web/dist
          ];
          extraSrc = [
            ./hscontrol/testdata
            ./hscontrol/types/testdata
            ./hscontrol/db/testdata
            ./hscontrol/policy/v2/testdata
          ];
        };
        goChecks = {
          build = fc.goBuild (common // {
            subPackages = [ "cmd/slopscale" ];
            env = { CGO_CFLAGS = sqliteCFlags; };
          });

          # The pure unit subset. ./integration (Docker) and
          # ./hscontrol/servertest (slow: 10s+ convergence plus race/stress/HA
          # property tests — run by the servertest workflow instead) are dropped
          # from the test set but kept in source so cmd/hi and friends still
          # compile; TestPostgres* needs a server (the SQLite equivalents still
          # run). The SQLite C flags match the build.
          gotest = fc.goTest (common // {
            testExclude = [ "/integration" "/hscontrol/servertest" ];
            goSkip = [ "TestPostgres" ];
            testEnv = "export CGO_CFLAGS=\"${sqliteCFlags}\"";
          });

          # Full-tree golangci-lint (golines, gofumpt, etc.); uses the overlay's
          # golangci-lint built against the pinned Go.
          golangci-lint = fc.goLint common;

          # nixpkgs-fmt only. goFmt = "off": Go formatting (golines, gofumpt)
          # is enforced by the golangci-lint check, not treefmt. Markup and
          # config files are formatted by oxfmt from the console toolchain
          # (`make lint-markup`, run by the admin console workflow), which
          # the sandboxed check cannot fetch.
          formatting = fc.goFormat (common // {
            goFmt = "off";
            prettier = false;
            fmtExclude = [ ./gen ./docs ./web ];
          });
        };
      in
      {
        # `nix develop`
        devShells.default = pkgs.mkShell {
          buildInputs =
            devDeps
            ++ [
              (pkgs.writeShellScriptBin
                "nix-vendor-sri"
                ''
                  set -eu
                  exec go run ./cmd/vendorhash update "$@"
                '')

              # cgo cross compiler for go build, goreleaser and ko: maps the
              # GOOS/GOARCH they set to a zig target so the SQLite C sources
              # compile for every Linux release target and link statically
              # against musl. Native builds use the platform compiler.
              (pkgs.writeShellScriptBin
                "zigcc"
                ''
                  set -eu
                  case "''${GOOS:-}/''${GOARCH:-}''${GOARM:+v$GOARM}" in
                    linux/amd64) target=x86_64-linux-musl ;;
                    linux/arm64) target=aarch64-linux-musl ;;
                    linux/arm | linux/armv7) target=arm-linux-musleabihf ;;
                    *)
                      echo "zigcc: no zig target for GOOS=''${GOOS:-} GOARCH=''${GOARCH:-}" >&2
                      exit 1
                      ;;
                  esac
                  # zig cc turns on UBSan for C by default and Go's linker has
                  # no runtime for it; the SQLite amalgamation is built without.
                  exec ${pkgs.zig}/bin/zig cc -target "$target" -fno-sanitize=undefined "$@"
                '')

              (pkgs.writeShellScriptBin
                "go-mod-update-all"
                ''
                  cat go.mod | ${pkgs.ripgrep}/bin/rg "\t" | ${pkgs.ripgrep}/bin/rg -v '^\s*//' | ${pkgs.ripgrep}/bin/rg -v indirect | ${pkgs.gawk}/bin/awk '{print $1}' | ${pkgs.findutils}/bin/xargs go get -u
                  go mod tidy
                '')
            ];

          shellHook = ''
            export PATH="$PWD/result/bin:$PATH"
            export CGO_ENABLED=1
            export CGO_CFLAGS="${sqliteCFlags}"
          '';
        };

        # `nix build`
        packages = with pkgs; {
          inherit slopscale;
          inherit slopscale-docker;
          default = slopscale;
        };

        # `nix run`
        apps.slopscale = flake-utils.lib.mkApp {
          drv = pkgs.slopscale;
        };
        apps.default = flake-utils.lib.mkApp {
          drv = pkgs.slopscale;
        };

        checks = {
          slopscale = pkgs.testers.nixosTest (import ./nix/tests/slopscale.nix);
        }
        # The Go build/test checks are gated to Linux: parts of the tree are
        # Linux-specific and the pure unit subset is validated by CI.
        // pkgs.lib.optionalAttrs pkgs.stdenv.hostPlatform.isLinux goChecks;
      });
}
