package hadolint

import (
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
)

func longs(arity getopt.Arity, names ...string) map[string]getopt.Arity {
	out := make(map[string]getopt.Arity, len(names))
	for _, name := range names {
		out[name] = arity
	}
	return out
}

func merged(tables ...map[string]getopt.Arity) map[string]getopt.Arity {
	out := map[string]getopt.Arity{}
	for _, table := range tables {
		maps.Copy(out, table)
	}
	return out
}

var aptGet = getopt.Grammar{Short: "a:bc:dfhmo:P:qst:uvVy", Long: merged(
	longs(getopt.Flag, "no-install-recommends", "install-recommends", "no-install-suggests", "install-suggests", "download-only",
		"fix-broken", "ignore-missing", "fix-missing", "no-download", "simulate", "just-print", "dry-run", "recon", "no-act",
		"yes", "assume-yes", "assume-no", "no-show-upgraded", "show-upgraded", "verbose-versions", "compile", "build",
		"ignore-hold", "with-new-pkgs", "no-upgrade", "only-upgrade", "allow-downgrades", "allow-remove-essential",
		"allow-change-held-packages", "force-yes", "print-uris", "purge", "reinstall", "list-cleanup", "no-list-cleanup",
		"trivial-only", "no-remove", "auto-remove", "autoremove", "only-source", "diff-only", "dsc-only", "tar-only",
		"arch-only", "indep-only", "allow-unauthenticated", "allow-insecure-repositories", "allow-releaseinfo-change",
		"show-progress", "fix-policy", "mark-auto", "help", "version"),
	longs(getopt.Valued, "host-architecture", "build-profiles", "target-release", "default-release", "config-file", "option",
		"with-source", "solver", "planner"),
	longs(getopt.Optional, "quiet"),
)}

var apk = getopt.Grammar{Short: "hiluUp:qst:vVX:", Long: merged(
	longs(getopt.Flag, "allow-untrusted", "force-binary-stdout", "force-broken-world", "force-missing-repositories",
		"force-no-chroot", "force-non-repository", "force-old-apk", "force-overwrite", "force-refresh", "help", "interactive",
		"no-interactive", "no-cache", "no-check-certificate", "no-logfile", "no-network", "no-progress", "preserve-env",
		"print-arch", "progress", "purge", "quiet", "update-cache", "verbose", "version", "wait", "clean-protected",
		"overlay-from-stdin", "no-scripts", "no-commit-hooks", "initdb", "usermode", "cache-packages", "legacy-info",
		"latest", "upgrade", "no-chown", "simulate", "available", "self-upgrade", "ignore", "prune", "recursive", "rdepends"),
	longs(getopt.Valued, "root", "repository", "arch", "cache-dir", "cache-max-age", "keys-dir", "progress-fd",
		"repositories-file", "timeout", "uvol-manager", "root-tmpfs", "virtual"),
)}

var yum = getopt.Grammar{Short: "hyqvbCc:d:e:R:x:", Long: merged(
	longs(getopt.Flag, "help", "assumeyes", "assumeno", "quiet", "verbose", "version", "best", "nobest", "cacheonly",
		"nogpgcheck", "noplugins", "obsoletes", "refresh", "skip-broken", "allowerasing", "nodocs", "downloadonly",
		"showduplicates", "security", "bugfix", "enhancement", "newpackage", "noautoremove", "skip-unavailable"),
	longs(getopt.Valued, "config", "debuglevel", "errorlevel", "installroot", "releasever", "exclude", "excludepkgs",
		"enablerepo", "disablerepo", "repo", "repoid", "setopt", "disableplugin", "enableplugin", "forcearch", "downloaddir",
		"destdir", "comment", "color", "rpmverbosity", "advisory", "advisories", "bz", "bzs", "cve", "cves", "sec-severity",
		"secseverity", "use-host-config", "from-repo"),
)}

var zypper = getopt.Grammar{Short: "hVqvnxtyc:D:R:r:fCdlbFp:", Long: merged(
	longs(getopt.Flag, "help", "version", "quiet", "verbose", "non-interactive", "non-interactive-include-reboot-patches",
		"no-confirm", "xmlout", "terse", "table-style", "no-abbrev", "no-gpg-checks", "gpg-auto-import-keys",
		"no-refresh", "no-cd", "no-remote", "auto-agree-with-licenses", "auto-agree-with-product-licenses", "force",
		"force-resolution", "no-force-resolution", "oldpackage", "download-only", "dry-run", "details",
		"no-recommends", "recommends", "allow-downgrade", "no-allow-downgrade", "allow-name-change",
		"allow-arch-change", "allow-vendor-change", "debug-solver", "clean-deps", "no-clean-deps", "all",
		"metadata", "raw-metadata", "build-deps-only", "no-build-deps", "replacefiles", "allow-unsigned-rpm",
		"ignore-unknown", "disable-repositories", "no-color", "plus-content", "skip-interactive", "with-interactive"),
	longs(getopt.Valued, "config", "root", "reposd-dir", "cache-dir", "raw-cache-dir", "solv-cache-dir", "pkg-cache-dir",
		"repo", "from", "type", "name", "plus-repo", "installroot", "userdata", "download", "rpm-root",
		"releasever", "category", "severity", "bugzilla", "cve", "date", "issues", "auto-agree-with-licenses-file"),
)}

var grammars = map[string]getopt.Grammar{
	"apt-get":  aptGet,
	"apt":      aptGet,
	"aptitude": aptGet,
	"apk":      apk,
	"yum":      yum,
	"dnf":      yum,
	"microdnf": yum,
	"zypper":   zypper,
}

func literal(w Word) (string, bool) { return w.Text, w.Literal }

type invocation struct {
	Command
	tool       string
	options    []getopt.Option[Word]
	subcommand *Word
	operands   []Word
}

func invoke(c Command) (invocation, bool) {
	name := c.Name()
	g, ok := grammars[name]
	if !ok {
		return invocation{}, false
	}
	parsed := getopt.Parse(g, c.Args(), literal)
	inv := invocation{Command: c, tool: name, options: parsed.Options, operands: parsed.Operands}
	if len(inv.operands) > 0 && inv.operands[0].Literal {
		inv.subcommand, inv.operands = &inv.operands[0], inv.operands[1:]
	}
	return inv, true
}

func (inv invocation) is(tools ...string) bool { return slices.Contains(tools, inv.tool) }

func (inv invocation) runs(subcommands ...string) bool {
	return inv.subcommand != nil && slices.Contains(subcommands, inv.subcommand.Text)
}

func (inv invocation) has(names ...string) bool {
	return slices.ContainsFunc(inv.options, func(o getopt.Option[Word]) bool { return slices.Contains(names, o.Name) })
}

func (inv invocation) value(names ...string) []string {
	var out []string
	for _, o := range inv.options {
		if !slices.Contains(names, o.Name) {
			continue
		}
		if text, ok := o.Argument.Text(literal); ok {
			out = append(out, text)
		}
	}
	return out
}

func enabled(value string) bool {
	return !slices.Contains([]string{"0", "no", "false", "off", "never"}, strings.ToLower(value))
}
