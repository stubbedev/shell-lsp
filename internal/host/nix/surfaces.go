package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

type role uint8

const (
	scriptRole role = iota
	sourcedRole
	phaseRole
	hookRole
	unitRole
	appRole
	runnerRole
	shebangRole
	namedRole
	gitAliasRole
	execRole
	foreignRole
)

const (
	anyAttr     = "*"
	unknownAttr = ""
	lastArg     = 0
)

type surface struct {
	attr     string
	call     string
	key      string
	arg      int
	role     role
	shell    dialect.Dialect
	language string
	provides []string
}

var (
	homeActivationValues   = []string{"DRY_RUN_CMD", "VERBOSE_ARG", "VERBOSE_ECHO", "VERBOSE_RUN", "newGenPath", "oldGenPath"}
	systemActivationValues = []string{"systemConfig"}
)

func providing(names []string, surfaces []surface) []surface {
	for i := range surfaces {
		surfaces[i].provides = names
	}
	return surfaces
}

func (s surface) evidence() string {
	if s.attr != "" {
		return s.attr
	}
	return s.call
}

func (s surface) detection() dialect.Detection {
	if s.shell == dialect.Bash {
		return dialect.Detection{Dialect: dialect.Bash, Source: dialect.SourceHostDefault, Evidence: tool}
	}
	return dialect.Detection{Dialect: s.shell, Source: dialect.SourceHostSetting, Evidence: s.evidence()}
}

func onAttrs(r role, shell dialect.Dialect, patterns ...string) []surface {
	out := make([]surface, len(patterns))
	for i, p := range patterns {
		out[i] = surface{attr: p, role: r, shell: shell}
	}
	return out
}

func under(prefix string, names ...string) []string {
	out := make([]string, len(names))
	for i, name := range names {
		out[i] = prefix + "." + name
	}
	return out
}

func onCalls(r role, shell dialect.Dialect, arg int, names ...string) []surface {
	out := make([]surface, len(names))
	for i, name := range names {
		out[i] = surface{call: name, arg: arg, role: r, shell: shell}
	}
	return out
}

func onKeys(r role, call string, keys ...string) []surface {
	out := make([]surface, len(keys))
	for i, key := range keys {
		out[i] = surface{call: call, key: key, role: r, shell: dialect.Bash}
	}
	return out
}

func foreignWriters(language string, names ...string) []surface {
	out := make([]surface, 0, 2*len(names))
	for _, name := range names {
		out = append(out, surface{call: name, role: foreignRole, language: language}, surface{call: name + "Bin", role: foreignRole, language: language})
	}
	return out
}

func foreignAttrs(language string, patterns ...string) []surface {
	out := onAttrs(foreignRole, dialect.Bash, patterns...)
	for i := range out {
		out[i].language = language
	}
	return out
}

const (
	shellApp      = "writeShellApplication"
	shellAppText  = "text"
	shellHook     = "shellHook"
	excludeChecks = "excludeShellChecks"
	writeTextFile = "writeTextFile"
	unitServices  = "systemd.services.*"
	userServices  = "systemd.user.services.*"
)

var (
	derivationBindings = phaseHooks(phaseStages, "buildCommand")
	unitScripts        = []string{"script", "preStart", "postStart", "preStop", "postStop", "reload"}
	execDirectives     = []string{"ExecStart", "ExecStartPre", "ExecStartPost", "ExecCondition", "ExecReload", "ExecStop", "ExecStopPost"}
	imageBuilders      = []string{"buildImage", "buildImageWithNixDb"}
	layeredBuilders    = []string{"buildLayeredImage", "buildLayeredImageWithNixDb", "streamLayeredImage"}
	fhsBuilders        = []string{"buildFHSEnv", "buildFHSEnvBubblewrap", "buildFHSEnvChroot"}
	nixosTests         = []string{"nixosTest", "runNixOSTest"}
)

func surfaceTable() []surface {
	var table [][]surface
	add := func(s ...[]surface) { table = append(table, s...) }
	add(
		onAttrs(phaseRole, dialect.Bash, derivationBindings...),
		onAttrs(hookRole, dialect.Bash, shellHook),
		onAttrs(unitRole, dialect.Bash, under(unitServices, unitScripts...)...),
		onAttrs(unitRole, dialect.Bash, under(userServices, unitScripts...)...),
		onAttrs(execRole, dialect.Posix, under(unitServices+".serviceConfig", execDirectives...)...),
		onAttrs(execRole, dialect.Posix, under(userServices+".serviceConfig", execDirectives...)...),
		onAttrs(execRole, dialect.Posix, under(userServices+".Service", execDirectives...)...),

		onCalls(scriptRole, dialect.Bash, 2, "writeShellScript", "writeShellScriptBin"),
		onCalls(scriptRole, dialect.Bash, lastArg, "writeBash", "writeBashBin"),
		onCalls(scriptRole, dialect.Dash, lastArg, "writeDash", "writeDashBin"),
		onCalls(shebangRole, dialect.Bash, 2, "writeScript", "writeScriptBin"),
		onCalls(runnerRole, dialect.Bash, 3, "runCommand", "runCommandLocal", "runCommandCC", "runCommandNoCC", "runCommandNoCCLocal"),
		onCalls(runnerRole, dialect.Bash, 2, "runCommandWith"),
		onCalls(namedRole, dialect.Bash, 2, "writeText", "writeTextDir", "toFile"),
		onKeys(appRole, shellApp, shellAppText),
		onKeys(namedRole, writeTextFile, shellAppText),

		onAttrs(namedRole, dialect.Bash, under("home.file.*", "text")...),
		onAttrs(namedRole, dialect.Bash, under("xdg.configFile.*", "text")...),
		onAttrs(namedRole, dialect.Bash, under("xdg.dataFile.*", "text")...),
		onAttrs(namedRole, dialect.Bash, under("xdg.stateFile.*", "text")...),
		onAttrs(namedRole, dialect.Bash, under("environment.etc.*", "text")...),

		onAttrs(sourcedRole, dialect.Bash, "home.file.*.onChange", "xdg.configFile.*.onChange", "xdg.dataFile.*.onChange", "xdg.stateFile.*.onChange"),
		providing(systemActivationValues, onAttrs(sourcedRole, dialect.Bash, "system.activationScripts.*", "system.activationScripts.*.text", "system.userActivationScripts.*", "system.userActivationScripts.*.text")),
		providing(homeActivationValues, onAttrs(sourcedRole, dialect.Bash, "home.activation.*")),
		onAttrs(sourcedRole, dialect.Bash, "home.extraProfileCommands", "home.extraBuilderCommands", "home.sessionVariablesExtra", "home.shellAliases.*"),
		onAttrs(sourcedRole, dialect.Bash, under("environment", "shellInit", "loginShellInit", "interactiveShellInit", "extraInit", "shellAliases.*")...),
		onAttrs(sourcedRole, dialect.Bash, under("programs.bash", "shellInit", "loginShellInit", "interactiveShellInit", "promptInit", "promptPluginInit", "logout", "bashrcExtra", "initExtra", "logoutExtra", "profileExtra", "shellAliases.*")...),
		onAttrs(sourcedRole, dialect.Zsh, under("programs.zsh", "shellInit", "loginShellInit", "interactiveShellInit", "promptInit", "completionInit", "envExtra", "initContent", "initExtra", "initExtraBeforeCompInit", "initExtraFirst", "loginExtra", "logoutExtra", "profileExtra", "shellAliases.*", "shellGlobalAliases.*", "siteFunctions.*")...),
		onAttrs(sourcedRole, dialect.Busybox, under("boot.initrd", "preDeviceCommands", "preFailCommands", "preLVMCommands", "postDeviceCommands", "postResumeCommands", "postMountCommands")...),
		onAttrs(sourcedRole, dialect.Bash, "boot.initrd.extraUtilsCommands", "boot.initrd.extraUtilsCommandsTest", "boot.initrd.extraUdevRulesCommands", "boot.postBootCommands"),
		onAttrs(sourcedRole, dialect.Bash, "boot.loader.grub.extraInstallCommands", "boot.loader.grub.extraPrepareConfig", "boot.loader.limine.extraInstallCommands", "boot.loader.systemd-boot.extraInstallCommands"),
		onAttrs(sourcedRole, dialect.Bash, under("networking", "localCommands", "firewall.extraCommands", "firewall.extraStopCommands", "nat.extraCommands", "nat.extraStopCommands", "dhcpcd.runHook")...),
		onAttrs(sourcedRole, dialect.Bash, under("networking.wireguard.interfaces.*", "preSetup", "postSetup", "preShutdown", "postShutdown")...),
		onAttrs(sourcedRole, dialect.Bash, under("networking.wg-quick.interfaces.*", "preUp", "postUp", "preDown", "postDown")...),
		onAttrs(sourcedRole, dialect.Bash, under("powerManagement", "bootCommands", "powerDownCommands", "powerUpCommands", "resumeCommands", "powertop.preStart", "powertop.postStart")...),
		onAttrs(sourcedRole, dialect.Bash, "services.xserver.displayManager.sessionCommands", "services.xserver.displayManager.setupCommands"),
		onAttrs(gitAliasRole, dialect.Posix, "programs.git.aliases.*", "programs.git.settings.alias.*", "programs.git.extraConfig.alias.*"),

		foreignAttrs("fish", under("programs.fish", "shellInit", "loginShellInit", "interactiveShellInit", "shellInitLast", "promptInit", "shellAliases.*", "shellAbbrs.*", "functions.*")...),
		foreignAttrs("nu", under("programs.nushell", "extraConfig", "extraEnv", "extraLogin", "shellAliases.*")...),
		foreignAttrs("python", "testScript"),
		foreignAttrs("udev rules", "services.udev.extraRules"),
		foreignWriters("fish", "writeFish"),
		foreignWriters("nu", "writeNu"),
		foreignWriters("python", "writePython3", "writePyPy2", "writePyPy3"),
		foreignWriters("perl", "writePerl"),
		foreignWriters("ruby", "writeRuby"),
		foreignWriters("lua", "writeLua"),
		foreignWriters("javascript", "writeJS"),
		foreignWriters("rust", "writeRust"),
		foreignWriters("haskell", "writeHaskell"),
		foreignWriters("nim", "writeNim"),
		foreignWriters("scheme", "writeGuile"),
		foreignWriters("clojure", "writeBabashka"),
		foreignWriters("fsharp", "writeFSharp"),
		foreignWriters("r", "writeRscript"),
		onCalls(foreignRole, dialect.Bash, lastArg, "writeCBin"),
	)
	for _, call := range imageBuilders {
		add(onKeys(scriptRole, call, "runAsRoot", "extraCommands"))
	}
	for _, call := range layeredBuilders {
		add(onKeys(scriptRole, call, "extraCommands", "fakeRootCommands"))
	}
	for _, call := range fhsBuilders {
		add(onKeys(scriptRole, call, "runScript", "profile", "extraBuildCommands", "extraInstallCommands", "extraPreBwrapCmds"))
	}
	for _, call := range nixosTests {
		add(onKeys(foreignRole, call, "testScript"))
	}
	var out []surface
	for _, part := range table {
		out = append(out, part...)
	}
	return out
}

type attrNode struct {
	kids    map[string]*attrNode
	any     *attrNode
	surface *surface
}

func (n *attrNode) kid(name string) *attrNode {
	if name == anyAttr {
		if n.any == nil {
			n.any = &attrNode{}
		}
		return n.any
	}
	if n.kids == nil {
		n.kids = map[string]*attrNode{}
	}
	next := n.kids[name]
	if next == nil {
		next = &attrNode{}
		n.kids[name] = next
	}
	return next
}

func (n *attrNode) find(path []string) *surface {
	if n.surface != nil {
		return n.surface
	}
	if len(path) == 0 {
		return nil
	}
	last, rest := path[len(path)-1], path[:len(path)-1]
	if last == unknownAttr {
		if n.any != nil {
			if found := n.any.find(rest); found != nil {
				return found
			}
		}
		return n.find(rest)
	}
	if next := n.kids[last]; next != nil {
		if found := next.find(rest); found != nil {
			return found
		}
	}
	if n.any != nil {
		return n.any.find(rest)
	}
	return nil
}

type callSurface struct {
	script *surface
	keys   map[string]*surface
}

func (c *callSurface) positional(k, args int) bool {
	if c.script == nil {
		return false
	}
	if c.script.arg == lastArg {
		return k == args && k >= 2
	}
	return k == c.script.arg
}

var attrSurfaces, callSurfaces = surfaceIndex(surfaceTable())

func surfaceIndex(table []surface) (*attrNode, map[string]*callSurface) {
	root := &attrNode{}
	calls := map[string]*callSurface{}
	for i := range table {
		s := &table[i]
		if s.attr != "" {
			node := root
			for _, segment := range slices.Backward(strings.Split(s.attr, ".")) {
				node = node.kid(segment)
			}
			node.surface = s
			continue
		}
		c := calls[s.call]
		if c == nil {
			c = &callSurface{keys: map[string]*surface{}}
			calls[s.call] = c
		}
		if s.key != "" {
			c.keys[s.key] = s
			continue
		}
		c.script = s
	}
	return root, calls
}

func positionalWriter(name string) bool {
	c := callSurfaces[name]
	if c == nil || c.script == nil {
		return false
	}
	return c.script.role == scriptRole || c.script.role == shebangRole
}

func runner(name string) bool {
	c := callSurfaces[name]
	return c != nil && c.script != nil && c.script.role == runnerRole
}

func knownWriter(name string) bool {
	c := callSurfaces[name]
	return c != nil && (c.script != nil || c.keys[shellAppText] != nil)
}

func scriptProducer(name string) bool { return knownWriter(name) && !runner(name) }

func scriptFence(name string) string {
	c := callSurfaces[name]
	if c == nil {
		return "sh"
	}
	s := c.script
	if s == nil {
		s = c.keys[shellAppText]
	}
	switch {
	case s == nil:
		return "sh"
	case s.role == foreignRole:
		return s.language
	case s.role == namedRole:
		return ""
	}
	return "sh"
}

var mainPrograms = map[string]string{
	"findutils": "find",
	"gnugrep":   "grep",
	"gnumake":   "make",
	"gnused":    "sed",
	"gnutar":    "tar",
	"nodejs":    "node",
	"openssh":   "ssh",
	"ripgrep":   "rg",
}

type carrier struct {
	arg   int
	items bool
}

var carriers = map[string]carrier{
	"mkBefore":            {arg: 1},
	"mkAfter":             {arg: 1},
	"mkForce":             {arg: 1},
	"mkDefault":           {arg: 1},
	"mkOverride":          {arg: 2},
	"mkOrder":             {arg: 2},
	"mkIf":                {arg: 2},
	"optionalString":      {arg: 2},
	"entryAfter":          {arg: 2},
	"entryBefore":         {arg: 2},
	"entryBetween":        {arg: 3},
	"entryAnywhere":       {arg: 1},
	"stringAfter":         {arg: 2},
	"noDepEntry":          {arg: 1},
	"fullDepEntry":        {arg: 1},
	"mkMerge":             {arg: 1, items: true},
	"concatStringsSep":    {arg: 2, items: true},
	"concatStrings":       {arg: 1, items: true},
	"concatLines":         {arg: 1, items: true},
	"concatMapStrings":    {arg: 1, items: true},
	"concatMapStringsSep": {arg: 2, items: true},
	"map":                 {arg: 1, items: true},
	"mapAttrsToList":      {arg: 1, items: true},
}

func stringWrap(name string) bool {
	c, known := carriers[name]
	return known && !c.items
}
