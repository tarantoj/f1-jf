# Builds the F1 IPTV proxy (f1iptv) and its companion m3u8-resolver CLI
# (f1m3u8) into a single derivation. f1iptv is the main program; runtime
# config comes from F1IPTV_* environment variables.
{ lib, buildGoModule }:

buildGoModule {
  pname = "f1iptv";
  version = "0.1.0";

  src = lib.fileset.toSource {
    root = ./.;
    fileset = lib.fileset.unions [
      ./go.mod
      ./go.sum
      ./cmd
      ./internal
    ];
  };

  subPackages = [
    "cmd/f1iptv"
    "cmd/f1m3u8"
  ];

  vendorHash = "sha256-EOkc+y94RqcdpOOIXjjImjQBKX6SBHy5sLJmx1I4s2g=";

  meta = {
    description = "F1 IPTV proxy exposing a Jellyfin M3U playlist";
    mainProgram = "f1iptv";
  };
}
