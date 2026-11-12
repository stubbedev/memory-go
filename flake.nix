{
  description = "memory-go - typed off-heap memory and containers for Go without cgo";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-parts = {
      url = "github:hercules-ci/flake-parts";
      inputs.nixpkgs-lib.follows = "nixpkgs";
    };
    devtools = {
      url = "github:stubbedev/flakes.nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-parts.follows = "flake-parts";
    };
  };

  outputs = inputs @ {
    flake-parts,
    devtools,
    ...
  }:
    flake-parts.lib.mkFlake {inherit inputs;} {
      systems = builtins.attrNames devtools.packages;
      imports = [
        ./nix/package.nix
        ./nix/shell.nix
      ];
    };
}
