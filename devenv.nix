{ pkgs, ... }:

{
  name = "f1-jf";

  # https://devenv.sh/basics/
  env.GREET = "devenv";

  # https://devenv.sh/packages/
  packages = with pkgs; [
    git # needed by the git-hooks (pre-commit) runner
  ];

  # https://devenv.sh/languages/
  languages.go.enable = true;
  languages.go.lsp.enable = true;

  # https://devenv.sh/processes/
  # processes.dev.exec = "go run ./cmd/f1iptv";

  # https://devenv.sh/git-hooks/
  git-hooks.hooks = {
    # Format Go source with gofmt.
    gofmt.enable = true;
    # Run `go vet` correctness checks.
    govet.enable = true;
    # Run the staticcheck static analyzer.
    staticcheck.enable = true;
    # Run the test suite for modified packages.
    gotest.enable = true;
    # Format Nix files (RFC 166 style).
    nixfmt-rfc-style.enable = true;
    # Find dead code in Nix files.
    deadnix.enable = true;
    # Lint Nix files.
    statix.enable = true;
    # Validate JSON syntax.
    check-json.enable = true;
    # Lint GitHub Actions workflow files.
    actionlint.enable = true;
  };

  # https://devenv.sh/scripts/
  scripts.hello.exec = ''
    echo hello from $GREET
  '';

  # https://devenv.sh/basics/
  enterShell = ''
    go version
  '';

  # https://devenv.sh/tests/
  enterTest = ''
    echo "Running tests"
    go test ./...
  '';

  # See full reference at https://devenv.sh/reference/options/
}
