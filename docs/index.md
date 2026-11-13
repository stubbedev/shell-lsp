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
  file: shell-finding-hover.png
  alt: A bash script with findings and inlay hints, and a hover that explains an unquoted command substitution and previews the fix
  caption: A finding in a bash script, with a hover that explains it and previews the fix.

features:
  - icon: '$_'
    title: Every shell by its own rules
    details: bash, zsh, POSIX sh, dash and ksh each have their own parser, and the dialect is detected per file from the shebang, a directive, .shellcheckrc or the file name. zsh is linted as zsh, which ShellCheck refuses to do.
    link: /shell/#dialects
    linkText: Dialects
  - icon: 'λ'
    title: Nix without an evaluator
    details: Flakes, module trees and overlays are followed from source, using 95 MiB at peak on an 89-file flake where nixd uses 576 MiB.
    link: /nix/
    linkText: Nix files
  - icon: '${ }'
    title: The languages are connected
    details: A recipe, a RUN instruction or a Nix string is linted and navigated as shell where it stands. A jq program in a shell script in a Nix string is one chain, from the jq variable to its flag, the shell variable and the Nix binding behind it.
    link: /hosts/#how-the-shell-is-mapped
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
  - file: zsh-pattern-hover.png
    alt: Hover on a zsh substitution listing each part of the pattern, the groups captured into $match, and the result 1:a 2:b
    title: Patterns explained
    text: Hover takes the shell's own patterns apart and, when the values are known, computes the result.
    link: /overview/#patterns-explained
    linkText: See the example
  - file: shell-completion-manpage.png
    alt: Completion after git switch -- listing its flags, with the manual page's description of --discard-changes
    title: Completion from the manual pages
    text: Flags and subcommands of installed commands come from their manual pages, read without man.
    link: /shell/#completion-and-hover
    linkText: Completion and hover
  - file: nix-finding-hover.png
    alt: A shell finding inside a writeShellScript string in a Nix file, with a hover that explains it and previews the fix
    title: A script inside a Nix string
    text: A writeShellScript string is read as shell with no marker, and findings land on the line of the .nix file.
    link: /overview/#a-script-inside-a-nix-string
    linkText: See the example
  - file: jq-peek-chain.png
    alt: Peek definition on a jq variable listing the jq flag, the shell variable and the Nix binding
    title: From a jq variable to the Nix binding behind it
    text: Peek definition on a jq variable lists the jq flag, the shell variable and the Nix binding.
    link: /overview/#from-a-jq-variable-to-the-nix-binding-behind-it
    linkText: See the example
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
  - value: 0.18 s
    label: To lint the 1,219 shell scripts of nixpkgs, where one shellcheck process takes 20.7 s
  - value: 95 MiB
    label: Peak memory on an 89-file flake, where nixd uses 576 MiB
  - value: 4,716
    label: Scripts in the pinned corpus that CI holds the linter to ShellCheck over
  - value: '0'
    label: Formatter differences from nixfmt 1.5.0 across all 44,765 files of nixpkgs
---
