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

      version = "0.1.0";

      # Source restricted to what the Go build needs. Must be a path literal
      # (not `self`): the repo has no commits yet, and `self` would resolve
      # via git to an empty tree.
      src = nixpkgs.lib.cleanSourceWith {
        src = ./.;
        filter =
          p: _:
          let
            base = baseNameOf (toString p);
          in
          !(builtins.elem base [
            ".git"
            "docs"
            "devenv.nix"
            "devenv.yaml"
            "devenv.lock"
            ".gitignore"
            "flake.nix"
            "flake.lock"
          ])
          && !(nixpkgs.lib.hasPrefix ".devenv" base);
      };
    in
    {
      packages = forAllSystems (
        system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          build =
            pname: description: subPackages:
            pkgs.buildGoModule {
              inherit
                pname
                version
                src
                subPackages
                description
                ;
              vendorHash = null; # no external Go dependencies
              meta.mainProgram = pname;
            };
        in
        {
          f1iptv = build "f1iptv" "F1 IPTV proxy exposing a Jellyfin M3U playlist" [
            "cmd/f1iptv"
          ];
          f1m3u8 = build "f1m3u8" "CLI that resolves F1 streams to m3u8 URLs" [
            "cmd/f1m3u8"
          ];
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
          default = pkgs.runCommand "f1-go-tests" { inherit src; } ''
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
