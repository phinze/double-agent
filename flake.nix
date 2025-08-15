{
  description = "Double Agent - SSH Agent Proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = {
    self,
    nixpkgs,
    flake-utils,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      devShells.default = pkgs.mkShell {
        buildInputs = with pkgs; [
          go
          golangci-lint
        ];

        shellHook = ''
          echo "Double Agent development environment"
          echo "Go version: $(go version)"
          echo ""
          echo "Available commands:"
          echo "  go build    - Build the project"
          echo "  go test     - Run tests"
          echo "  go run      - Run the application"
          echo ""
        '';
      };
    });
}

