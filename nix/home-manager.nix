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

    socketPath = mkOption {
      type = types.str;
      default = "$HOME/.ssh/agent";
      description = "Path where the proxy socket will be created.";
    };

    verbose = mkOption {
      type = types.bool;
      default = false;
      description = "Enable verbose logging.";
    };

    autoStart = mkOption {
      type = types.bool;
      default = true;
      description = "Automatically start double-agent on session login.";
    };

    shellIntegration = {
      bash = mkOption {
        type = types.bool;
        default = true;
        description = "Enable bash shell integration.";
      };

      zsh = mkOption {
        type = types.bool;
        default = true;
        description = "Enable zsh shell integration.";
      };

      fish = mkOption {
        type = types.bool;
        default = true;
        description = "Enable fish shell integration.";
      };
    };
  };

  config = mkIf cfg.enable {
    home.packages = [ cfg.package ];

    # Systemd user service
    systemd.user.services.double-agent = mkIf cfg.autoStart {
      Unit = {
        Description = "Double Agent - SSH Agent Proxy";
        Documentation = "https://github.com/phinze/double-agent";
        After = [ "graphical-session-pre.target" ];
        PartOf = [ "graphical-session.target" ];
      };

      Service = {
        Type = "simple";
        ExecStart = "${cfg.package}/bin/double-agent ${optionalString cfg.verbose "-v"} ${cfg.socketPath}";
        Restart = "always";
        RestartSec = 5;
      };

      Install = {
        WantedBy = [ "default.target" ];
      };
    };

    # Shell integrations
    programs.bash.initExtra = mkIf cfg.shellIntegration.bash ''
      # Double Agent integration
      export DOUBLE_AGENT_SOCKET="${cfg.socketPath}"
      if [ -S "$DOUBLE_AGENT_SOCKET" ]; then
        export SSH_AUTH_SOCK="$DOUBLE_AGENT_SOCKET"
      fi
    '';

    programs.zsh.initExtra = mkIf cfg.shellIntegration.zsh ''
      # Double Agent integration
      export DOUBLE_AGENT_SOCKET="${cfg.socketPath}"
      if [[ -S "$DOUBLE_AGENT_SOCKET" ]]; then
        export SSH_AUTH_SOCK="$DOUBLE_AGENT_SOCKET"
      fi
    '';

    programs.fish.shellInit = mkIf cfg.shellIntegration.fish ''
      # Double Agent integration
      set -gx DOUBLE_AGENT_SOCKET "${cfg.socketPath}"
      if test -S "$DOUBLE_AGENT_SOCKET"
        set -gx SSH_AUTH_SOCK "$DOUBLE_AGENT_SOCKET"
      end
    '';
  };
}
