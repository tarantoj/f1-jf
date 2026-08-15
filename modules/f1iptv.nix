# NixOS module for the F1 IPTV proxy. Configure via services.f1iptv.* and
# enable the service. The default `package` is the flake's own f1iptv build.
{
  config,
  lib,
  package,
}:

let
  cfg = config.services.f1iptv;
in
{
  options.services.f1iptv = {
    enable = lib.mkEnableOption "F1 IPTV proxy";

    package = lib.mkOption {
      type = lib.types.package;
      default = package;
      defaultText = lib.literalExpression "the flake's f1iptv package";
      description = "The f1iptv package to run.";
    };

    listen = lib.mkOption {
      type = lib.types.str;
      default = ":8080";
      description = "Address to listen on (F1IPTV_LISTEN).";
    };

    qualities = lib.mkOption {
      type = lib.types.str;
      default = "1080p,720p";
      description = "Comma-separated qualities to expose as channels (F1IPTV_QUALITIES).";
    };

    sourceURL = lib.mkOption {
      type = lib.types.str;
      default = "https://streamfree.top/embed/racing/skyf1";
      description = "Embed source URL to resolve (F1IPTV_SOURCE_URL).";
    };

    baseURL = lib.mkOption {
      type = lib.types.str;
      default = "";
      description = "External base URL for playlist URLs (F1IPTV_BASE_URL); empty derives from the request Host.";
    };

    group = lib.mkOption {
      type = lib.types.str;
      default = "Sports";
      description = "IPTV group-title for the channels (F1IPTV_GROUP).";
    };

    ttl = lib.mkOption {
      type = lib.types.str;
      default = "30s";
      example = "1m";
      description = "How long to cache a resolved stream before refreshing tokens (F1IPTV_TTL).";
    };

    verifyPlaylist = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "GET each resolved playlist to confirm it is reachable (F1IPTV_VERIFY_PLAYLIST).";
    };

    logLevel = lib.mkOption {
      type = lib.types.enum [
        "debug"
        "info"
        "warn"
        "error"
      ];
      default = "info";
      description = "Log verbosity (F1IPTV_LOG_LEVEL).";
    };

    openFirewall = lib.mkOption {
      type = lib.types.bool;
      default = false;
      description = "Open the listen port in networking.firewall.allowedTCPPorts.";
    };
  };

  config = lib.mkIf cfg.enable {
    systemd.services.f1iptv = {
      description = "F1 IPTV proxy";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];

      serviceConfig = {
        ExecStart = "${lib.getExe cfg.package}";
        Restart = "on-failure";
        RestartSec = "5s";
        DynamicUser = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        PrivateTmp = true;
        NoNewPrivileges = true;
        # The proxy must reach the public internet (source discovery + HLS).
        PrivateNetwork = false;
      };

      environment = {
        F1IPTV_LISTEN = cfg.listen;
        F1IPTV_QUALITIES = cfg.qualities;
        F1IPTV_SOURCE_URL = cfg.sourceURL;
        F1IPTV_BASE_URL = cfg.baseURL;
        F1IPTV_GROUP = cfg.group;
        F1IPTV_TTL = cfg.ttl;
        F1IPTV_VERIFY_PLAYLIST = if cfg.verifyPlaylist then "true" else "false";
        F1IPTV_LOG_LEVEL = cfg.logLevel;
      };
    };

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall (
      lib.optional (builtins.match ".*:([0-9]+)" cfg.listen != null) (
        lib.toInt (builtins.elemAt (builtins.match ".*:([0-9]+)" cfg.listen) 0)
      )
    );
  };
}
