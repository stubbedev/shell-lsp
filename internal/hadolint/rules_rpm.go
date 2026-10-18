package hadolint

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/lint"
)

var (
	dnfTools        = []string{"yum", "dnf", "microdnf"}
	dnfFamilies     = [][]string{{"yum", "dnf"}, {"microdnf"}}
	dnfCaches       = []string{"/var/cache/yum", "/var/cache/dnf", "/var/cache/libdnf5"}
	dnfDownloading  = []string{"install", "in", "groupinstall", "localinstall", "reinstall", "update", "upgrade", "up", "update-minimal", "upgrade-minimal", "downgrade", "distro-sync", "dsync", "swap"}
	dnfTransactions = append(slices.Clone(dnfDownloading), "remove", "rm", "erase", "autoremove")
	dnfNested       = []string{"group", "groups", "module"}
	dnfNestedTasks  = []string{"install", "remove", "upgrade", "update", "enable", "disable", "reset", "switch-to", "mark"}
	dnfInstalling   = []string{"install", "in", "localinstall", "reinstall"}

	zypperCache        = "/var/cache/zypp"
	zypperDownloading  = []string{"install", "in", "update", "up", "patch", "dist-upgrade", "dup", "source-install", "si", "install-new-recommends", "inr"}
	zypperTransactions = append(slices.Clone(zypperDownloading), "remove", "rm", "verify", "ve")
	zypperInstalling   = []string{"install", "in"}
	zypperKinds        = []string{"pattern", "patch", "product", "srcpackage", "application"}
)

func (inv invocation) dnfAssumesYes() bool {
	if inv.has("-y", "--assumeyes", "--assumeno") {
		return true
	}
	for _, v := range inv.value("--setopt") {
		name, value, _ := strings.Cut(v, "=")
		if name == "assumeyes" && enabled(value) {
			return true
		}
	}
	return false
}

func (inv invocation) dnfTransaction() bool {
	if inv.runs(dnfNested...) {
		return len(inv.operands) > 0 && slices.Contains(dnfNestedTasks, inv.operands[0].Text)
	}
	return inv.runs(dnfTransactions...)
}

func (inv invocation) dnfStream() bool {
	return inv.runs("module") && len(inv.operands) > 0 && inv.operands[0].Text == "install"
}

func dnfPinned(stream bool) func(string) bool {
	return func(name string) bool {
		return strings.HasPrefix(name, "@") || strings.HasSuffix(name, ".rpm") || stream && strings.Contains(name, ":") || versioned(name)
	}
}

func versioned(name string) bool {
	for i := 1; i+1 < len(name); i++ {
		if name[i] == '-' && name[i+1] >= '0' && name[i+1] <= '9' {
			return true
		}
	}
	return false
}

func (c *checker) rpm() {
	for _, run := range c.Runs {
		c.dnf(run)
		c.zypper(run)
	}
}

func (c *checker) dnf(run *Run) {
	invs := c.invocations(run, dnfTools...)
	cachedAway := slices.ContainsFunc(dnfCaches, func(dir string) bool { return run.removes(dir) || run.cached(dir) })
	cleans := func(tool string) bool {
		family := dnfFamilies[slices.IndexFunc(dnfFamilies, func(f []string) bool { return slices.Contains(f, tool) })]
		return cachedAway || slices.ContainsFunc(invs, func(inv invocation) bool { return inv.is(family...) && inv.runs("clean") })
	}
	reported := false
	for _, inv := range invs {
		yes, clean, pin := lint.DL3038, lint.DL3040, lint.DL3041
		if inv.tool == "yum" {
			yes, clean, pin = lint.DL3030, lint.DL3032, lint.DL3033
		}
		if inv.dnfTransaction() && !inv.dnfAssumesYes() {
			c.report(yes, inv.subcommand.Span, "`%s %s` stops to ask for confirmation, which fails the build; add `-y`", inv.tool, inv.subcommand.Text)
		}
		if inv.runs(dnfDownloading...) && c.persists(run) && !cleans(inv.tool) && !reported {
			reported = true
			c.report(clean, inv.subcommand.Span, "the packages and metadata `%s %s` downloads stay in the image; run `%s clean all` in the same RUN", inv.tool, inv.subcommand.Text, inv.tool)
		}
		stream := inv.dnfStream()
		if !inv.runs(dnfInstalling...) && !stream {
			continue
		}
		packages := inv.operands
		if stream {
			packages = packages[1:]
		}
		c.unpinned(run, packages, dnfPinned(stream), func(pkg Word, name string) {
			c.report(pin, pkg.Span, "pin the version of `%s`, as in `%s-<version>`, so a rebuild installs the same package", name, name)
		})
	}
}

func zypperPinned(name string) bool {
	kind, _, typed := strings.Cut(name, ":")
	return strings.ContainsAny(name, "=<>") || strings.HasSuffix(name, ".rpm") || typed && slices.Contains(zypperKinds, kind)
}

func (c *checker) zypper(run *Run) {
	invs := c.invocations(run, "zypper")
	cleaned := slices.ContainsFunc(invs, func(inv invocation) bool { return inv.runs("clean", "cc") }) ||
		run.removes(zypperCache) || run.cached(zypperCache)
	reported := false
	for _, inv := range invs {
		if inv.runs("dist-upgrade", "dup") {
			c.report(lint.DL3035, inv.subcommand.Span, "`zypper %s` upgrades the whole base image to another release; pin a newer base image instead", inv.subcommand.Text)
		}
		if inv.runs(zypperTransactions...) && !inv.has("-n", "--non-interactive", "-y", "--no-confirm") {
			c.report(lint.DL3034, inv.subcommand.Span, "`zypper %s` stops to ask for confirmation, which fails the build; add `--non-interactive`", inv.subcommand.Text)
		}
		if inv.runs(zypperDownloading...) && c.persists(run) && !cleaned && !reported {
			reported = true
			c.report(lint.DL3036, inv.subcommand.Span, "the packages and metadata `zypper %s` downloads stay in the image; run `zypper clean --all` in the same RUN", inv.subcommand.Text)
		}
		if !inv.runs(zypperInstalling...) || slices.ContainsFunc(inv.value("-t", "--type"), func(kind string) bool { return kind != "package" }) {
			continue
		}
		c.unpinned(run, inv.operands, zypperPinned, func(pkg Word, name string) {
			c.report(lint.DL3037, pkg.Span, "pin the version of `%s`, as in `%s=<version>`, so a rebuild installs the same package", name, name)
		})
	}
}
