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

      vendorHash = "sha256-2VtfS8bs9veC15ItKz1enHqsfkT3J2rH+z+t+rOLbR4=";

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
