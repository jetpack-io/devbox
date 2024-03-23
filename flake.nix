{
  description = "Devbox: Reproducible Dev Environments";
  inputs = {
    nixpkgs.url =
      "https://github.com/nixos/nixpkgs/archive/3954218cf613eba8e0dcefa9abe337d26bc48fd0.tar.gz";

    flake-utils.url = "github:numtide/flake-utils";

    gomod2nix = {
      url = "github:nix-community/gomod2nix";
      inputs.nixpkgs.follows = "nixpkgs";
      inputs.flake-utils.follows = "flake-utils";
    };
  };

  outputs = { self, nixpkgs, flake-utils, gomod2nix }:
    let

      lastModifiedDate = self.lastModifiedDate or self.lastModified or "19700101";
      # System types to support.
      supportedSystems =
        [ "x86_64-linux" "x86_64-darwin" "aarch64-linux" "aarch64-darwin" ];

      # Helper function to generate an attrset '{ x86_64-linux = f "x86_64-linux"; ... }'.
      forAllSystems = nixpkgs.lib.genAttrs supportedSystems;

      # Nixpkgs instantiated for supported system types.
      nixpkgsFor = forAllSystems (system: import nixpkgs {
        inherit system;
        overlays = [ gomod2nix.overlays.default ];
      });

      commit =
        if builtins.hasAttr "shortRev" self
        then
          builtins.substring 0 7 self.shortRev
        else
          "precommit-${lastModifiedDate}";

      semver = "0.4.2";
    in
    {

      # Provide some binary packages for selected system types.
      packages = forAllSystems (system:
        let
          pkgs = nixpkgsFor.${system};
          version = "${semver}.${commit}";
          pname = "devbox";
          name = "devbox-${version}";

        in
        {
          devbox = pkgs.buildGoApplication {
            inherit pname;
            inherit name;
            inherit version;

            nativeBuildInputs = with pkgs; [ installShellFiles ];

            src = ./.;

            modules = ./gomod2nix.toml;

            # integration tests want filesystem access
            doCheck = false;

            ldflags = [
              "-s"
              "-w"
              "-X go.jetpack.io/devbox/internal/build.Version=${version}"
            ];

            postInstall = ''
              installShellCompletion --cmd devbox \
                --bash <($out/bin/devbox completion bash) \
                --fish <($out/bin/devbox completion fish) \
                --zsh <($out/bin/devbox completion zsh)
            '';
          };
        });

      # Add dependencies that are only needed for development
      devShells = forAllSystems (system:
        let pkgs = nixpkgsFor.${system};
        in
        {
          default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go_1_19
              gopls
              gotools
              go-tools
              golangci-lint
              gomod2nix.packages.${system}.default
            ];
          };
        });

      defaultPackage = forAllSystems (system: self.packages.${system}.devbox);
    };
}
