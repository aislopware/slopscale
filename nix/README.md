# Slopscale NixOS Module

This directory contains the NixOS module for Slopscale.

## Rationale

The module is maintained in this repository to keep the code and module
synchronized at the same commit. This allows faster iteration and ensures the
module stays compatible with the latest Slopscale changes. All changes should
aim to be upstreamed to nixpkgs.

## Files

- **[`module.nix`](./module.nix)** - The NixOS module implementation
- **[`example-configuration.nix`](./example-configuration.nix)** - Example
  configuration demonstrating all major features
- **[`tests/`](./tests/)** - NixOS integration tests

## Usage

Add to your flake inputs:

```nix
inputs.slopscale.url = "github:aislopware/slopscale";
```

Then import the module:

```nix
imports = [ inputs.slopscale.nixosModules.default ];
```

See [`example-configuration.nix`](./example-configuration.nix) for configuration
options.

## Upstream

- [nixpkgs module](https://github.com/NixOS/nixpkgs/blob/master/nixos/modules/services/networking/slopscale.nix)
- [nixpkgs package](https://github.com/NixOS/nixpkgs/blob/master/pkgs/by-name/he/slopscale/package.nix)

The module in this repository may be newer than the nixpkgs version.
