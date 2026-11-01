{self, ...}: {
  perSystem = {
    lib,
    pkgs,
    ...
  }: {
    packages.default = pkgs.buildGo127Module {
      pname = "memory-go";
      version = "0-unstable-${self.lastModifiedDate or "0"}";

      src = lib.fileset.toSource {
        root = ../.;
        fileset = lib.fileset.difference (lib.fileset.gitTracked ../.) (
          lib.fileset.unions [
            ../flake.nix
            ../flake.lock
            ../nix
          ]
        );
      };

      vendorHash = "sha256-HEkYXZajQ2x64GUv8LpVxiaCJBEu/0i9MUVX3F77AuA=";

      env.CGO_ENABLED = 0;

      doCheck = true;

      meta = {
        description = "Typed off-heap memory and containers for Go without cgo";
        homepage = "https://github.com/stubbedev/memory-go";
        license = lib.licenses.mit;
        platforms = lib.platforms.unix;
      };
    };
  };
}
