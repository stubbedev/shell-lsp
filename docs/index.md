---
layout: home
title: shell-lsp
titleTemplate: One language server for shell and every language that embeds it

hero:
  name: shell-lsp
  tagline: One language server for shell and every language that embeds it
  actions:
    - theme: brand
      text: Get started
      link: /overview/#quick-start
    - theme: alt
      text: GitHub
      link: https://github.com/stubbedev/shell-lsp

languages: [bash, zsh, POSIX sh, dash, ksh, justfile, Makefile, Dockerfile, Nix]

screenshot:
  file: nix-finding-hover.png
  alt: A shell finding inside a writeShellScript string in a Nix file, with a hover that explains it and previews the fix
  caption: A shell finding inside a Nix string, with a hover that explains it and previews the fix.

features:
  - icon: '$_'
    title: Every shell by its own rules
    details: bash, zsh, POSIX sh, dash and ksh each have their own parser, and the dialect is detected per file from the shebang, a directive, .shellcheckrc or the file name. zsh is linted as zsh, which ShellCheck refuses to do.
    link: /overview/#dialects
    linkText: Dialects
  - icon: 'λ'
    title: Nix without an evaluator
    details: Flakes, module trees and overlays are followed from source, using 95 MiB at peak on an 89-file flake where nixd uses 576 MiB.
    link: /nix/
    linkText: Nix files
  - icon: '${ }'
    title: The languages are connected
    details: A recipe, a RUN instruction or a Nix string is linted and navigated as shell where it stands. A jq program in a shell script in a Nix string is one chain, from the jq variable to its flag, the shell variable and the Nix binding behind it.
    link: /overview/#how-embedded-languages-work
    linkText: How embedded languages work
  - icon: '1'
    title: One static binary replaces a toolbox
    details: The linter, the formatters and the manual-page reader are built in, so shellcheck, shfmt, man, Hadolint, statix, deadnix and nixfmt do not need to be installed. Their check codes, directives and flags keep working.
    link: /overview/#how-it-compares
    linkText: How it compares
  - icon: '0Δ'
    title: Compatibility is measured
    details: CI holds the linter to ShellCheck over a pinned corpus of 4,716 scripts, and the Nix formatter has 0 differences from nixfmt 1.5.0 across all 44,765 files of nixpkgs.
    link: /corpus/
    linkText: Corpus checks
  - icon: 'SL'
    title: Every check is listed
    details: The linter has 459 SC checks, 143 SL checks and 54 DL checks, each listed with its severity. The same binary lints and formats from the command line.
    link: /checks/
    linkText: All checks

showcase:
  - file: jq-peek-chain.png
    alt: Peek definition on a jq variable listing the jq flag, the shell variable and the Nix binding
    title: From a jq variable to the Nix binding behind it
    text: Peek definition on a jq variable lists the jq flag, the shell variable and the Nix binding.
    link: /overview/#from-a-jq-variable-to-the-nix-binding-behind-it
    linkText: See the example
  - file: nix-peek-pkgs-jq.png
    alt: Peek definition from pkgs.jq in a shell string opening package.nix from the locked nixpkgs
    title: A jump into nixpkgs, with nothing evaluated
    text: Definition on a package opens it in the nixpkgs that flake.lock pins, read from its source in the Nix store.
    link: /overview/#a-jump-into-nixpkgs-with-nothing-evaluated
    linkText: See the example
  - file: nix-rename-refused.png
    alt: 'A refused rename: symbol cannot be renamed, the module system passes lib to this module by this name'
    title: A rename that refuses, and says why
    text: The module system passes lib by name, so instead of breaking the module, the server names what would break.
    link: /nix/#rename
    linkText: Rename in Nix
  - file: dockerfile-problems.png
    alt: A Dockerfile with squiggles and a Problems panel listing DL and SC findings next to findings from a justfile, a script and a Nix file
    title: Findings from every file, together
    text: Hadolint's rules, BuildKit's checks and the shell in RUN report together, next to what the other open files found.
    link: /hosts/
    linkText: Justfiles, Makefiles and Dockerfiles
  - file: cli-check.png
    alt: The check command in a terminal, reporting findings in a Nix file and a shell script with colours and links
    title: The same findings on the command line
    text: The check command reports what the editor shows, for every language at once.
    link: /cli/
    linkText: Command line

numbers:
  - value: 95 MiB
    label: Peak memory on an 89-file flake, where nixd uses 576 MiB
  - value: '0'
    label: Formatter differences from nixfmt 1.5.0 across all 44,765 files of nixpkgs
  - value: 4,716
    label: Scripts in the pinned corpus that CI holds the linter to ShellCheck over
---
