{
  description = "Double Agent - A self-healing SSH agent proxy";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem
      (system:
        let
          pkgs = nixpkgs.legacyPackages.${system};
          double-agent = pkgs.callPackage ./nix/package.nix { };
        in
        {
          packages = {
            default = double-agent;
            double-agent = double-agent;
          };

          apps.default = flake-utils.lib.mkApp {
            drv = double-agent;
          };

          formatter = pkgs.nixpkgs-fmt;

          devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              gotools
              go-tools
              golangci-lint
              gnumake
            ];
          };
        }
      ) // {
      homeManagerModules.default = import ./nix/home-manager.nix self;
      nixosModules.default = import ./nix/nixos.nix self;
    };
}
