{
  description = "postitt — fast personal command reference / picker";
  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };
  outputs =
    {
      self,
      nixpkgs,
      flake-utils,
    }:
    flake-utils.lib.eachDefaultSystem (
      system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
      in
      {
        packages.default = pkgs.buildGoModule {
          pname = "postitt";
          version = "0.1.0";
          src = ./.;
          # Run nix build once with vendorHash = pkgs.lib.fakeHash,
          # then replace this with the hash it prints.
          vendorHash = "sha256-QTDFivZw3N8Zzb1w7asf0nwySY7fvVUNiKVWTItLMXY=";
          # modernc.org/sqlite is pure Go — no CGo needed.
          env.CGO_ENABLED = "0";
          meta = {
            description = "Personal command reference: a fast picker for saved commands";
            homepage = "https://github.com/LordHerdier/postitt-cli";
            mainProgram = "postitt";
          };
        };
        devShells.default = pkgs.mkShell {
          buildInputs = with pkgs; [
            go
            gopls
            gotools
          ];
        };
      }
    );
}
