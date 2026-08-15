{
  description = "F1 IPTV microservice: proxies live F1 streams as a Jellyfin IPTV source";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs =
    { self, nixpkgs }:
    let
      supportedSystems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          f1iptv = pkgs.callPackage ./package.nix { };
          default = self.packages.${system}.f1iptv;
        }
      );

      devShells = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default = pkgs.mkShell {
            packages = [
              pkgs.go
              pkgs.git
            ];
            shellHook = ''
              go version
            '';
          };
        }
      );

      # The module is a function of the NixOS module arguments so it can close
      # over the flake's own f1iptv package as the default.
      nixosModules.default =
        {
          config,
          lib,
          pkgs,
          ...
        }:
        import ./modules/f1iptv.nix {
          inherit config lib;
          package = self.packages.${pkgs.stdenv.hostPlatform.system}.f1iptv;
        };

      checks = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
        in
        {
          default =
            pkgs.runCommand "f1-go-tests"
              {
                src = self.packages.${system}.f1iptv.src;
              }
              ''
                cp -r $src src
                chmod -R u+w src
                cd src
                export HOME=$TMPDIR
                ${pkgs.go}/bin/go test ./...
                touch $out
              '';
        }
      );
    };
}
