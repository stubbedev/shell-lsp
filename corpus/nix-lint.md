# Nix lint comparison

shell-lsp's Nix checks against statix and deadnix, written by `just corpus-nix-lint-baseline`;
see docs/corpus.md. Positions only, one difference a line.

2500 files compared, 0 left out because a parser rejected them.

| Lint | Agree | Ours only | Theirs only |
| --- | ---: | ---: | ---: |
| bool_comparison | 0 | 0 | 0 |
| bool_simplification | 0 | 0 | 0 |
| collapsible_let_in | 0 | 0 | 0 |
| deadnix unused argument | 105 | 0 | 0 |
| deadnix unused let | 13 | 0 | 0 |
| deadnix unused pattern | 174 | 0 | 0 |
| deprecated_to_path | 0 | 0 | 0 |
| empty_inherit | 0 | 0 | 0 |
| empty_let_in | 0 | 0 | 0 |
| empty_list_concat | 6 | 0 | 0 |
| empty_pattern | 43 | 0 | 0 |
| eta_reduction | 2 | 0 | 0 |
| legacy_let_syntax | 0 | 0 | 0 |
| manual_inherit | 15 | 0 | 0 |
| manual_inherit_from | 81 | 0 | 0 |
| redundant_pattern_bind | 1 | 0 | 0 |
| repeated_keys | 81 | 0 | 0 |
| unquoted_uri | 0 | 0 | 0 |
| useless_has_attr | 1 | 1 | 0 |
| useless_parens | 63 | 64 | 0 |

| Check of ours alone | Findings |
| --- | ---: |
| SL8030 | 14 |
| SL8035 | 38 |
| SL8036 | 35 |
| SL8037 | 4 |

Fixes in files up to 128 KiB that broke the file or changed what a name means: 0.

## Differences

nixos/modules/i18n/input-method/fcitx5.nix:84:34 useless_parens ours
nixos/modules/misc/man-db.nix:85:18 useless_parens ours
nixos/modules/programs/pay-respects.nix:39:7 useless_parens ours
nixos/modules/programs/pay-respects.nix:60:8 useless_parens ours
nixos/modules/services/mail/maddy.nix:444:14 useless_parens ours
nixos/modules/services/mail/maddy.nix:452:19 useless_parens ours
nixos/modules/services/mail/maddy.nix:460:19 useless_parens ours
nixos/modules/services/matrix/synapse-auto-compressor.nix:41:30 useless_has_attr ours
nixos/modules/services/matrix/synapse-auto-compressor.nix:41:33 useless_parens ours
nixos/modules/services/misc/gammu-smsd.nix:54:5 useless_parens ours
nixos/modules/services/misc/weechat.nix:33:12 useless_parens ours
nixos/modules/services/networking/mycelium.nix:110:18 useless_parens ours
nixos/modules/services/networking/syncplay.nix:165:44 useless_parens ours
nixos/modules/services/networking/syncplay.nix:196:13 useless_parens ours
nixos/modules/services/security/fail2ban.nix:20:19 useless_parens ours
nixos/modules/services/web-apps/matomo.nix:265:14 useless_parens ours
nixos/modules/services/web-apps/matomo.nix:267:19 useless_parens ours
nixos/modules/services/web-apps/snipe-it.nix:463:22 useless_parens ours
nixos/modules/services/web-apps/snipe-it.nix:476:22 useless_parens ours
nixos/modules/services/x11/hardware/synaptics.nix:231:13 useless_parens ours
nixos/modules/system/boot/luksroot.nix:377:53 useless_parens ours
nixos/modules/tasks/network-interfaces.nix:588:12 useless_parens ours
nixos/modules/tasks/network-interfaces.nix:613:12 useless_parens ours
pkgs/applications/editors/emacs/build-support/melpa.nix:76:16 useless_parens ours
pkgs/build-support/fetchgitiles/default.nix:16:10 useless_parens ours
pkgs/build-support/fetchhg/default.nix:37:12 useless_parens ours
pkgs/build-support/fetchhg/default.nix:40:11 useless_parens ours
pkgs/by-name/ar/arpack/package.nix:43:12 useless_parens ours
pkgs/by-name/br/brioche/package.nix:7:17 useless_parens ours
pkgs/by-name/cr/cryptpad/package.nix:40:12 useless_parens ours
pkgs/by-name/cy/cyclonedx-cli/package.nix:36:37 useless_parens ours
pkgs/by-name/dp/dpdk/package.nix:24:13 useless_parens ours
pkgs/by-name/fi/fim/package.nix:17:16 useless_parens ours
pkgs/by-name/go/gomobile/package.nix:13:17 useless_parens ours
pkgs/by-name/id/identity/package.nix:89:23 useless_parens ours
pkgs/by-name/ko/koboldcpp/package.nix:32:28 useless_parens ours
pkgs/by-name/ro/roc-toolkit/package.nix:72:10 useless_parens ours
pkgs/by-name/ro/rofimoji/package.nix:8:20 useless_parens ours
pkgs/by-name/ro/rofimoji/package.nix:9:16 useless_parens ours
pkgs/by-name/sd/SDL2_ttf/package.nix:12:19 useless_parens ours
pkgs/by-name/td/tdb/package.nix:73:10 useless_parens ours
pkgs/by-name/te/telepresence2/package.nix:44:43 useless_parens ours
pkgs/by-name/te/test-drive/package.nix:13:8 useless_parens ours
pkgs/by-name/ty/typst/build-universe-package.nix:38:21 useless_parens ours
pkgs/by-name/ty/typst/wrapper.nix:12:16 useless_parens ours
pkgs/by-name/wa/wannier90/package.nix:14:8 useless_parens ours
pkgs/by-name/x2/x265/package.nix:12:17 useless_parens ours
pkgs/by-name/xp/xpra/package.nix:200:5 useless_parens ours
pkgs/by-name/zo/zoom-us/package.nix:16:29 useless_parens ours
pkgs/desktops/lomiri/applications/lomiri-system-settings/default.nix:219:10 useless_parens ours
pkgs/development/compilers/gcc/common/configure-flags.nix:38:22 useless_parens ours
pkgs/development/compilers/gcc/common/configure-flags.nix:114:15 useless_parens ours
pkgs/development/compilers/gcc/common/configure-flags.nix:143:11 useless_parens ours
pkgs/development/compilers/gcc/common/configure-flags.nix:219:10 useless_parens ours
pkgs/development/misc/resholve/python2/mk-python-derivation.nix:83:24 useless_parens ours
pkgs/development/mobile/androidenv/deploy-androidpackages.nix:76:12 useless_parens ours
pkgs/development/python-modules/matplotlib/default.nix:44:14 useless_parens ours
pkgs/development/python-modules/numba-cuda/default.nix:42:8 useless_parens ours
pkgs/development/rocq-modules/coq-elpi/default.nix:17:7 useless_parens ours
pkgs/servers/home-assistant/custom-lovelace-modules/advanced-camera-card/package.nix:27:9 useless_parens ours
pkgs/servers/monitoring/zabbix/zabbix-agent2-plugins/mssql.nix:25:12 useless_parens ours
pkgs/tools/networking/openssh/common.nix:47:22 useless_parens ours
pkgs/tools/package-management/nix/modular/packaging/components.nix:404:8 useless_parens ours
pkgs/tools/security/ghidra/with-extensions.nix:25:5 useless_parens ours
pkgs/tools/typesetting/tex/texlive/tlpdb-overrides.nix:41:7 useless_parens ours
