self: { config, lib, pkgs, ... }:

with lib;
let
  cfg = config.services.double-agent;
in
{
  options.services.double-agent = {
    enable = mkEnableOption "double-agent SSH agent proxy";

    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.system}.double-agent;
      defaultText = literalExpression "pkgs.double-agent";
      description = "The double-agent package to use.";
    };

    users = mkOption {
      type = types.listOf types.str;
      default = [ ];
      example = [ "alice" "bob" ];
      description = "List of users for whom to enable double-agent.";
    };

    socketPath = mkOption {
      type = types.str;
      default = ".ssh/agent";
      description = "Path relative to user's home where the proxy socket will be created.";
    };

    verbose = mkOption {
      type = types.bool;
      default = false;
      description = "Enable verbose logging.";
    };
  };

  config = mkIf cfg.enable {
    environment.systemPackages = [ cfg.package ];

    systemd.user.services.double-agent = {
      description = "Double Agent - SSH Agent Proxy";
      documentation = [ "https://github.com/phinze/double-agent" ];
      after = [ "multi-user.target" ];
      wantedBy = [ "default.target" ];

      serviceConfig = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/double-agent ${optionalString cfg.verbose "-v"} %h/${cfg.socketPath}";
        Restart = "always";
        RestartSec = 5;
      };
    };
  };
}
