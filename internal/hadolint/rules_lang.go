package hadolint

import (
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

var pip = getopt.Grammar{Short: "C:c:e:f:hi:Ir:t:qUv", Long: merged(
	longs(getopt.Flag, "upgrade", "user", "no-deps", "pre", "force-reinstall", "ignore-installed", "ignore-requires-python",
		"no-build-isolation", "use-pep517", "no-use-pep517", "check-build-dependencies", "break-system-packages", "compile",
		"no-compile", "no-warn-script-location", "no-warn-conflicts", "prefer-binary", "require-hashes", "no-clean",
		"no-cache-dir", "quiet", "verbose", "disable-pip-version-check", "no-color", "no-input", "isolated", "no-index",
		"dry-run", "no-python-version-warning", "help", "version", "no-binary-all", "debug"),
	longs(getopt.Valued, "requirement", "constraint", "editable", "target", "platform", "python-version", "implementation", "abi",
		"root", "prefix", "src", "upgrade-strategy", "index-url", "extra-index-url", "find-links", "no-binary", "only-binary",
		"progress-bar", "report", "config-settings", "global-option", "log", "proxy", "retries", "timeout", "exists-action",
		"trusted-host", "cert", "client-cert", "cache-dir", "python", "root-user-action", "keyring-provider", "group"),
)}

var npm = getopt.Grammar{Short: "BDEgOPSdqsw:", Long: merged(
	longs(getopt.Flag, "global", "save", "save-dev", "save-exact", "save-optional", "save-prod", "save-peer", "save-bundle", "no-save",
		"production", "legacy-peer-deps", "strict-peer-deps", "force", "ignore-scripts", "no-audit", "audit", "no-fund", "fund",
		"silent", "quiet", "dry-run", "prefer-offline", "prefer-online", "offline", "no-package-lock", "package-lock-only",
		"foreground-scripts", "no-bin-links", "install-links", "global-style", "legacy-bundling", "verbose", "workspaces",
		"include-workspace-root", "no-optional", "no-progress", "progress", "unsafe-perm"),
	longs(getopt.Valued, "loglevel", "prefix", "registry", "cache", "tag", "userconfig", "globalconfig", "workspace", "omit",
		"include", "install-strategy", "before", "cpu", "os", "libc", "fetch-retries", "fetch-timeout", "location", "script-shell"),
)}

var gem = getopt.Grammar{Short: "ab:EfFg:i:lNn:p:P:qrs:uVv:w", Long: merged(
	longs(getopt.Flag, "no-document", "no-ri", "no-rdoc", "user-install", "no-user-install", "env-shebang", "no-env-shebang", "force",
		"no-force", "conservative", "minimal-deps", "pre", "prerelease", "quiet", "verbose", "clear-sources", "default", "development",
		"development-all", "ignore-dependencies", "both", "local", "remote", "wrappers", "no-wrappers", "lock", "no-lock", "suggestions",
		"no-suggestions", "post-install-message", "no-post-install-message", "update-sources", "no-update-sources", "vendor",
		"explain", "silent", "norc", "format-executable", "no-format-executable"),
	longs(getopt.Valued, "version", "install-dir", "bindir", "document", "source", "http-proxy", "platform", "file", "build-root",
		"trust-policy", "target-rbconfig", "config-file"),
)}

var yarn = getopt.Grammar{Short: "DEOPTW", Long: merged(
	longs(getopt.Flag, "dev", "exact", "optional", "peer", "tilde", "ignore-workspace-root-check", "frozen-lockfile", "immutable",
		"pure-lockfile", "production", "prod", "ignore-scripts", "ignore-engines", "ignore-optional", "ignore-platform", "force",
		"flat", "har", "no-lockfile", "non-interactive", "offline", "prefer-offline", "silent", "verbose", "check-files",
		"inline-builds", "json", "mode", "skip-builds", "update-checksums"),
	longs(getopt.Valued, "cwd", "cache-folder", "modules-folder", "network-timeout", "registry", "mutex", "link-folder", "global-folder"),
)}

var goValued = []string{
	"C", "o", "p", "tags", "ldflags", "gcflags", "asmflags", "mod", "modfile", "pkgdir", "toolexec", "buildmode",
	"compiler", "installsuffix", "overlay", "pgo", "coverpkg", "covermode", "exec",
}

const (
	pipCache  = "/.cache/pip"
	yarnCache = "/.cache/yarn"
)

func pipCommand(cmd Command) ([]Word, bool) {
	name, args := cmd.Name(), cmd.Args()
	if name == "pipx" || name == "pip" || strings.HasPrefix(name, "pip") && strings.Trim(name[len("pip"):], "0123456789.") == "" {
		return args, true
	}
	if !strings.HasPrefix(name, "python") || strings.Trim(name[len("python"):], "0123456789.") != "" {
		return nil, false
	}
	for i := 0; i+1 < len(args); i++ {
		switch {
		case args[i].Text == "-m" && args[i+1].Text == "pip":
			return args[i+2:], true
		case !strings.HasPrefix(args[i].Text, "-"):
			return nil, false
		}
	}
	return nil, false
}

func subcommand(parsed getopt.Parsed[Word]) (Word, []Word, bool) {
	if len(parsed.Operands) == 0 || !parsed.Operands[0].Literal {
		return Word{}, nil, false
	}
	return parsed.Operands[0], parsed.Operands[1:], true
}

func (c *checker) languages() {
	for _, run := range c.Runs {
		yarnCleaned := run.cachedUnder(yarnCache) || slices.ContainsFunc(run.Commands, func(cmd Command) bool {
			parsed := parse(yarn, cmd)
			sub, rest, ok := subcommand(parsed)
			return cmd.Name() == "yarn" && ok && sub.Text == "cache" && len(rest) > 0 && rest[0].Text == "clean"
		})
		yarnReported := false
		for _, cmd := range run.Commands {
			if args, ok := pipCommand(cmd); ok {
				c.pip(run, cmd, args)
			}
			switch cmd.Name() {
			case "npm":
				c.npm(run, cmd)
			case "gem":
				c.gem(run, cmd)
			case "go":
				c.goInstall(run, cmd)
			case "yarn":
				if !yarnCleaned && !yarnReported && c.persists(run) && c.yarnInstalls(cmd) {
					yarnReported = true
					c.report(lint.DL3060, cmd.Words[cmd.Start].Span, "the yarn cache stays in the image; run `yarn cache clean` in the same RUN")
				}
			}
		}
	}
}

func (c *checker) words(run *Run, w Word) ([]string, bool) {
	if w.Literal {
		return []string{w.Text}, true
	}
	value, known := c.value(run, w, 0)
	return strings.Fields(value), known
}

func (c *checker) unpinned(run *Run, packages []Word, pinned func(string) bool, report func(pkg Word, name string)) {
	for _, pkg := range packages {
		names, known := c.words(run, pkg)
		if !known {
			continue
		}
		for _, name := range names {
			if !pinned(name) {
				report(pkg, name)
			}
		}
	}
}

func (c *checker) pip(run *Run, cmd Command, args []Word) {
	parsed := getopt.Parse(pip, args, literal)
	sub, packages, ok := subcommand(parsed)
	if !ok || sub.Text != "install" {
		return
	}
	if cmd.Name() != "pipx" && c.persists(run) && !parsed.Has("--no-cache-dir") && !run.cachedUnder(pipCache) && !c.pipCacheOff(run, cmd) {
		c.report(lint.DL3042, sub.Span, "pip keeps the downloaded packages in its cache inside the image; add `--no-cache-dir`")
	}
	if parsed.Has("-r", "--requirement", "-c", "--constraint") {
		return
	}
	c.unpinned(run, packages, pipPinned, func(pkg Word, name string) {
		c.report(lint.DL3013, pkg.Span, "pin the version of `%s`, as in `%s==<version>`, so a rebuild installs the same package", name, name)
	})
}

var pipBooleans = []string{"y", "yes", "t", "true", "on", "1", "n", "no", "f", "false", "off", "0"}

func pipBoolean(value string) bool { return slices.Contains(pipBooleans, strings.ToLower(value)) }

func (c *checker) pipCacheOff(run *Run, cmd Command) bool {
	for _, a := range cmd.Assigns {
		if name, value, _ := strings.Cut(a, "="); name == "PIP_NO_CACHE_DIR" && pipBoolean(value) {
			return true
		}
	}
	value, known := c.File.Expand(run.Instruction, "${PIP_NO_CACHE_DIR}")
	return known && pipBoolean(value)
}

var (
	pipSpecifiers = []string{"==", "~=", "!=", ">", "<"}
	vcsSchemes    = []string{"git+", "hg+", "svn+", "bzr+"}
)

func pipPinned(name string) bool {
	if slices.ContainsFunc(vcsSchemes, func(scheme string) bool { return strings.HasPrefix(name, scheme) }) {
		_, address, _ := strings.Cut(name, "://")
		return strings.Contains(address[strings.IndexByte(address, '/')+1:], "@")
	}
	return slices.ContainsFunc(pipSpecifiers, func(op string) bool { return strings.Contains(name, op) }) || strings.Contains(name, "@") ||
		strings.ContainsAny(name, "/") || name == "." || archive(name) || strings.HasSuffix(name, ".whl") || strings.HasSuffix(name, ".zip")
}

func (c *checker) npm(run *Run, cmd Command) {
	parsed := parse(npm, cmd)
	sub, packages, ok := subcommand(parsed)
	if !ok || !slices.Contains([]string{"install", "i", "add", "in", "ins", "inst", "insta", "instal", "isnt", "isnta", "isntal", "isntall"}, sub.Text) {
		return
	}
	c.unpinned(run, packages, npmPinned, func(pkg Word, name string) {
		c.report(lint.DL3016, pkg.Span, "pin the version of `%s`, as in `%s@<version>`, so a rebuild installs the same package", name, name)
	})
}

func npmPinned(spec string) bool {
	switch {
	case strings.Contains(spec, "://") || strings.HasPrefix(spec, "git@") || strings.HasPrefix(spec, "github:") ||
		strings.HasPrefix(spec, "gitlab:") || strings.HasPrefix(spec, "bitbucket:") || strings.HasPrefix(spec, "gist:"):
		return strings.Contains(spec, "#") || strings.HasSuffix(spec, ".tgz") || strings.HasSuffix(spec, ".tar.gz")
	case spec == "." || spec == ".." || strings.HasPrefix(spec, "/") || strings.HasPrefix(spec, "./") || strings.HasPrefix(spec, "../") ||
		strings.HasPrefix(spec, "~/") || strings.HasPrefix(spec, "file:") || archive(spec):
		return true
	case strings.HasPrefix(spec, "@"):
		_, rest, scoped := strings.Cut(spec, "/")
		return scoped && npmVersioned(rest)
	case !strings.Contains(spec, "@") && strings.Count(spec, "/") == 1:
		return strings.Contains(spec, "#")
	}
	return npmVersioned(spec)
}

func npmVersioned(spec string) bool {
	_, version, ok := strings.Cut(spec, "@")
	return ok && version != "" && version != latest && version != "next"
}

func (c *checker) gem(run *Run, cmd Command) {
	args := cmd.Args()
	if end := slices.IndexFunc(args, func(w Word) bool { return w.Literal && w.Text == getopt.EndOfOptions }); end >= 0 {
		args = args[:end]
	}
	parsed := getopt.Parse(gem, args, literal)
	sub, gems, ok := subcommand(parsed)
	if !ok || sub.Text != "install" && sub.Text != "i" || parsed.Has("-v", "--version") {
		return
	}
	c.unpinned(run, gems, gemPinned, func(g Word, name string) {
		c.report(lint.DL3028, g.Span, "pin the version of `%s`, as in `%s:<version>` or with `-v`, so a rebuild installs the same gem", name, name)
	})
}

func gemPinned(name string) bool {
	return strings.Contains(name, ":") || strings.HasSuffix(name, ".gem")
}

func (c *checker) yarnInstalls(cmd Command) bool {
	sub, _, ok := subcommand(parse(yarn, cmd))
	return !ok && len(cmd.Args()) == 0 || ok && (sub.Text == "install" || sub.Text == "add")
}

func goOperands(args []Word) []Word {
	var out []Word
	for i := 0; i < len(args); i++ {
		text := args[i].Text
		switch {
		case text == "--":
			return append(out, args[i+1:]...)
		case strings.HasPrefix(text, "-") && args[i].Literal:
			name, _, attached := strings.Cut(strings.TrimLeft(text, "-"), "=")
			if !attached && slices.Contains(goValued, name) {
				i++
			}
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func (c *checker) goInstall(run *Run, cmd Command) {
	args := cmd.Args()
	if len(args) == 0 || !args[0].Literal {
		return
	}
	sub := args[0].Text
	if sub != "install" && sub != "get" && sub != "run" {
		return
	}
	packages := goOperands(args[1:])
	if sub == "run" && len(packages) > 1 {
		packages = packages[:1]
	}
	c.unpinned(run, packages, goPinned, func(pkg Word, name string) {
		module, _, _ := strings.Cut(name, "@")
		c.report(lint.DL3062, pkg.Span, "pin the version of `%s`, as in `%s@<version>`, so a rebuild installs the same module", module, module)
	})
}

func goPinned(pkg string) bool {
	if pkg == "." || strings.HasPrefix(pkg, "./") || strings.HasPrefix(pkg, "../") || path.IsAbs(pkg) || strings.HasSuffix(pkg, ".go") {
		return true
	}
	_, version, ok := strings.Cut(pkg, "@")
	return ok && version != latest && version != "none" && version != "upgrade" && version != "patch"
}
