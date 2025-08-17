{ lib, buildGoModule }:

buildGoModule {
  pname = "double-agent";
  version = "0.1.0";

  src = ./..;

  vendorHash = null; # No external dependencies

  meta = with lib; {
    description = "A self-healing SSH agent proxy for tmux and long-running sessions";
    homepage = "https://github.com/phinze/double-agent";
    license = licenses.asl20;
    maintainers = [ ];
    mainProgram = "double-agent";
  };
}
