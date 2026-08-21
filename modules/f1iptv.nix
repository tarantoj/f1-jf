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

    host = lib.mkOption {
      type = lib.types.str;
      default = "";
      example = "127.0.0.1";
      description = "Address to bind (F1IPTV_HOST); empty binds all interfaces.";
    };

    port = lib.mkOption {
      type = lib.types.port;
      default = 8080;
      description = "Port to listen on (F1IPTV_PORT).";
    };

    qualities = lib.mkOption {
      type = lib.types.str;
      default = "2160p,1080p,720p";
      description = "Comma-separated ordered fallback list of qualities to try across every source (F1IPTV_QUALITIES).";
    };

    dashboardURL = lib.mkOption {
      type = lib.types.str;
      default = "https://f1net.vercel.app";
      description = "Dashboard origin the source list is fetched from (F1IPTV_DASHBOARD_URL).";
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

    epgEnable = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Serve the XMLTV guide at /iptv/guide.xml (F1IPTV_EPG_ENABLED).";
    };

    epgAPIURL = lib.mkOption {
      type = lib.types.str;
      default = "https://api.openf1.org/v1";
      description = "OpenF1 API base URL for the season calendar (F1IPTV_EPG_API_URL).";
    };

    epgTTL = lib.mkOption {
      type = lib.types.str;
      default = "6h";
      example = "1d";
      description = "How long to cache the season calendar (F1IPTV_EPG_TTL).";
    };

    epgYear = lib.mkOption {
      type = lib.types.int;
      default = 0;
      example = 2026;
      description = "Season year for the guide; 0 uses the current year (F1IPTV_EPG_YEAR).";
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
        F1IPTV_HOST = cfg.host;
        F1IPTV_PORT = toString cfg.port;
        F1IPTV_QUALITIES = cfg.qualities;
        F1IPTV_DASHBOARD_URL = cfg.dashboardURL;
        F1IPTV_BASE_URL = cfg.baseURL;
        F1IPTV_GROUP = cfg.group;
        F1IPTV_TTL = cfg.ttl;
        F1IPTV_VERIFY_PLAYLIST = if cfg.verifyPlaylist then "true" else "false";
        F1IPTV_LOG_LEVEL = cfg.logLevel;
        F1IPTV_EPG_ENABLED = if cfg.epgEnable then "true" else "false";
        F1IPTV_EPG_API_URL = cfg.epgAPIURL;
        F1IPTV_EPG_TTL = cfg.epgTTL;
        F1IPTV_EPG_YEAR = toString cfg.epgYear;
      };
    };

    networking.firewall.allowedTCPPorts = lib.mkIf cfg.openFirewall [ cfg.port ];
  };
}
