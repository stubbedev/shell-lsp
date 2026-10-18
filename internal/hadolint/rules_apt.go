package hadolint

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/dockerfile"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

const aptLists = "/var/lib/apt/lists"

var (
	aptPrompting   = []string{"install", "upgrade", "dist-upgrade", "full-upgrade", "remove", "purge", "autoremove", "autopurge", "build-dep", "satisfy"}
	assumeYesNames = []string{"-y", "--yes", "--assume-yes"}
	quietNames     = []string{"-q", "--quiet"}
	nonInteractive = []string{"-s", "--simulate", "--just-print", "--dry-run", "--recon", "--no-act", "--assume-no"}
)

func (inv invocation) aptConfig(key string) (string, bool) {
	for _, v := range inv.value("-o", "--option") {
		name, value, ok := strings.Cut(v, "=")
		if ok && strings.EqualFold(name, key) {
			return value, true
		}
	}
	return "", false
}

func (inv invocation) aptQuiet() int {
	level := 0
	for _, o := range inv.options {
		text, _ := literal(o.Word)
		if !slices.Contains(quietNames, o.Name) && !strings.HasPrefix(text, "-q=") {
			continue
		}
		if _, value, ok := strings.Cut(text, "="); ok {
			n, err := strconv.Atoi(value)
			if err == nil {
				level = n
			}
			continue
		}
		level++
	}
	return level
}

func (inv invocation) assumesYes() bool {
	for _, o := range inv.options {
		if !slices.Contains(assumeYesNames, o.Name) {
			continue
		}
		value, attached := o.Argument.Text(literal)
		if !attached || enabled(value) {
			return true
		}
	}
	if value, ok := inv.aptConfig("APT::Get::Assume-Yes"); ok && enabled(value) {
		return true
	}
	return inv.aptQuiet() >= 2 || inv.has(nonInteractive...)
}

func (inv invocation) recommends() bool {
	if inv.has("--no-install-recommends", "--install-recommends") {
		return false
	}
	value, ok := inv.aptConfig("APT::Install-Recommends")
	return !ok || enabled(value)
}

func aptPinned(name string) bool {
	return strings.ContainsAny(name, "=/") || strings.HasSuffix(name, ".deb") || strings.HasSuffix(name, "-")
}

func (c *checker) apt() {
	for _, run := range c.Runs {
		updated := false
		invs := c.invocations(run, "apt-get", "apt", "aptitude")
		listsCleared := run.removes(aptLists) || run.cached(aptLists) ||
			slices.ContainsFunc(invs, func(inv invocation) bool { return inv.runs("dist-clean") })
		for _, inv := range invs {
			if inv.tool == "apt" {
				c.report(lint.DL3027, inv.Words[inv.Start].Span, "`apt` is meant for people at a terminal and its output may change between releases; use `apt-get` or `apt-cache` in scripts")
			}
			if inv.runs("update") && !updated && c.persists(run) && !listsCleared {
				updated = true
				c.report(lint.DL3009, inv.subcommand.Span, "the package lists `%s update` downloads stay in the image; remove %s/* in the same RUN", inv.tool, aptLists)
			}
			if inv.tool != "apt-get" {
				continue
			}
			if inv.runs(aptPrompting...) && !inv.assumesYes() {
				c.report(lint.DL3014, inv.subcommand.Span, "`apt-get %s` stops to ask for confirmation, which fails the build; add `-y`", inv.subcommand.Text)
			}
			if !inv.runs("install") {
				continue
			}
			if inv.recommends() {
				c.report(lint.DL3015, inv.subcommand.Span, "`apt-get install` also installs recommended packages; add `--no-install-recommends` to install only what is listed")
			}
			c.unpinned(run, inv.operands, aptPinned, func(pkg Word, name string) {
				c.report(lint.DL3008, pkg.Span, "pin the version of `%s`, as in `%s=<version>`, so a rebuild installs the same package", name, name)
			})
		}
	}
}

func (c *checker) persists(run *Run) bool {
	if run.Trigger != nil || run.Stage == len(c.stages)-1 {
		return true
	}
	return slices.ContainsFunc(c.stages, func(s dockerfile.Stage) bool { return s.Parent == run.Stage })
}

const apkCache = "/var/cache/apk"

func apkPinned(name string) bool {
	return strings.ContainsAny(name, "=<>~") || strings.HasSuffix(name, ".apk")
}

func (c *checker) apk() {
	for _, run := range c.Runs {
		for _, inv := range c.invocations(run, "apk") {
			if !inv.runs("add") {
				continue
			}
			if c.persists(run) && !inv.has("--no-cache", "--no-network") && !run.cached(apkCache) && !run.removes(apkCache) {
				c.report(lint.DL3019, inv.subcommand.Span, "`apk add` keeps the package index in %s; add `--no-cache`", apkCache)
			}
			c.unpinned(run, inv.operands, apkPinned, func(pkg Word, name string) {
				c.report(lint.DL3018, pkg.Span, "pin the version of `%s`, as in `%s=<version>`, so a rebuild installs the same package", name, name)
			})
		}
	}
}
