package nix

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

const (
	makeWrapperInput      = "makeWrapper"
	installShellFilesName = "installShellFiles"
)

type hookFlag struct {
	name, usage, doc string
}

var wrapperFlags = []hookFlag{
	{"--argv0", "--argv0 NAME", "Sets the name of the executed process."},
	{"--inherit-argv0", "--inherit-argv0", "Passes the wrapper's own `argv0` on to the executable."},
	{"--resolve-argv0", "--resolve-argv0", "Resolves an `argv0` without a slash against `PATH`."},
	{"--set", "--set VAR VAL", "Sets `VAR` in the executable's environment."},
	{"--set-default", "--set-default VAR VAL", "Sets `VAR` only when the environment does not have it yet."},
	{"--unset", "--unset VAR", "Removes `VAR` from the environment."},
	{"--chdir", "--chdir DIR", "Changes the working directory first."},
	{"--run", "--run COMMAND", "Runs a shell command before the executable."},
	{"--add-flag", "--add-flag ARG", "Puts the single argument `ARG` before the caller's arguments."},
	{"--append-flag", "--append-flag ARG", "Puts the single argument `ARG` after the caller's arguments."},
	{"--add-flags", "--add-flags ARGS", "Puts `ARGS`, read by Bash, before the caller's arguments."},
	{"--append-flags", "--append-flags ARGS", "Puts `ARGS`, read by Bash, after the caller's arguments."},
	{"--prefix", "--prefix VAR SEP VAL", "Puts `VAL` at the front of `VAR`, separated by `SEP`."},
	{"--suffix", "--suffix VAR SEP VAL", "Puts `VAL` at the end of `VAR`, separated by `SEP`."},
	{"--prefix-each", "--prefix-each VAR SEP VALS", "Puts each of the words of `VALS` at the front of `VAR`."},
	{"--suffix-each", "--suffix-each VAR SEP VALS", "Puts each of the words of `VALS` at the end of `VAR`."},
	{"--prefix-contents", "--prefix-contents VAR SEP FILES", "Puts the contents of `FILES` at the front of `VAR`."},
	{"--suffix-contents", "--suffix-contents VAR SEP FILES", "Puts the contents of `FILES` at the end of `VAR`."},
}

var substituteFlags = []hookFlag{
	{"--replace-fail", "--replace-fail OLD NEW", "Replaces `OLD` with `NEW`, and fails the build when `OLD` is absent."},
	{"--replace-warn", "--replace-warn OLD NEW", "Replaces `OLD` with `NEW`, and warns when `OLD` is absent."},
	{"--replace-quiet", "--replace-quiet OLD NEW", "Replaces `OLD` with `NEW`, and says nothing when `OLD` is absent."},
	{"--subst-var", "--subst-var VAR", "Replaces `@VAR@` with the value of the shell variable `VAR`."},
	{"--subst-var-by", "--subst-var-by VAR VAL", "Replaces `@VAR@` with `VAL`."},
}

type hookCommand struct {
	name, label, doc, provider string
	params                     []string
	flags                      []hookFlag
}

var hookCommands = []hookCommand{
	{name: "wrapProgram", label: "wrapProgram EXECUTABLE ARGS", doc: "Moves `EXECUTABLE` aside and writes a wrapper in its place.", provider: makeWrapperInput, params: []string{"EXECUTABLE", "ARGS"}, flags: wrapperFlags},
	{name: "makeWrapper", label: "makeWrapper EXECUTABLE OUT ARGS", doc: "Writes a wrapper at `OUT` that runs `EXECUTABLE`.", provider: makeWrapperInput, params: []string{"EXECUTABLE", "OUT", "ARGS"}, flags: wrapperFlags},
	{name: "substituteInPlace", label: "substituteInPlace FILE ARGS", doc: "Rewrites `FILE` with the replacements its arguments give.", params: []string{"FILE", "ARGS"}, flags: substituteFlags},
	{name: "substitute", label: "substitute INPUT OUTPUT ARGS", doc: "Writes `INPUT` to `OUTPUT` with the replacements its arguments give.", params: []string{"INPUT", "OUTPUT", "ARGS"}, flags: substituteFlags},
	{name: "substituteAll", label: "substituteAll INPUT OUTPUT", doc: "Writes `INPUT` to `OUTPUT`, replacing `@VAR@` for every environment variable.", params: []string{"INPUT", "OUTPUT"}},
	{name: "substituteAllInPlace", label: "substituteAllInPlace FILE", doc: "Rewrites `FILE`, replacing `@VAR@` for every environment variable.", params: []string{"FILE"}},
	{name: "patchShebangs", label: "patchShebangs PATHS", doc: "Points the interpreters of the scripts under `PATHS` at the store.", params: []string{"PATHS"}},
	{name: "runHook", label: "runHook NAME", doc: "Runs the hooks registered under `NAME`, such as `preBuild`.", params: []string{"NAME"}},
	{name: "addToSearchPath", label: "addToSearchPath VAR DIR", doc: "Adds `DIR` to the colon-separated `VAR` when `DIR` exists.", params: []string{"VAR", "DIR"}},
	{name: "prependToVar", label: "prependToVar VAR VALUES", doc: "Puts `VALUES` at the front of `VAR`, as an array or a string as `VAR` is one.", params: []string{"VAR", "VALUES"}},
	{name: "appendToVar", label: "appendToVar VAR VALUES", doc: "Puts `VALUES` at the end of `VAR`, as an array or a string as `VAR` is one.", params: []string{"VAR", "VALUES"}},
	{name: "stripHash", label: "stripHash PATH", doc: "Prints the name of a store path without its hash.", params: []string{"PATH"}},
	{name: "unpackFile", label: "unpackFile FILE", doc: "Unpacks `FILE` into the working directory.", params: []string{"FILE"}},
	{name: "addEnvHooks", label: "addEnvHooks OFFSET HOOKS", doc: "Registers functions that run for each dependency.", params: []string{"OFFSET", "HOOKS"}},
	{name: "unpackPhase", label: "unpackPhase", doc: "Runs the default unpack phase of `stdenv`."},
	{name: "patchPhase", label: "patchPhase", doc: "Runs the default patch phase of `stdenv`."},
	{name: "configurePhase", label: "configurePhase", doc: "Runs the default configure phase of `stdenv`."},
	{name: "buildPhase", label: "buildPhase", doc: "Runs the default build phase of `stdenv`."},
	{name: "checkPhase", label: "checkPhase", doc: "Runs the default check phase of `stdenv`."},
	{name: "installPhase", label: "installPhase", doc: "Runs the default install phase of `stdenv`."},
	{name: "fixupPhase", label: "fixupPhase", doc: "Runs the default fixup phase of `stdenv`."},
	{name: "installBin", label: "installBin FILES", doc: "Installs `FILES` under `$out/bin`.", provider: installShellFilesName, params: []string{"FILES"}},
	{name: "installManPage", label: "installManPage FILES", doc: "Installs manual pages under `$out/share/man`, by their section.", provider: installShellFilesName, params: []string{"FILES"}},
	{name: "installShellCompletion", label: "installShellCompletion FILES", doc: "Installs shell completions where each shell looks for them.", provider: installShellFilesName, params: []string{"FILES"}},
}

type HookCommand struct {
	Builtin  model.Builtin
	Provider string
}

func commandOf(name string) (hookCommand, bool) {
	i := slices.IndexFunc(hookCommands, func(c hookCommand) bool { return c.name == name })
	if i < 0 {
		return hookCommand{}, false
	}
	return hookCommands[i], true
}

func (c hookCommand) public() HookCommand {
	return HookCommand{Provider: c.provider, Builtin: model.Builtin{Name: c.name, Kind: model.KindFunction, Signature: c.label, Doc: c.doc, Params: c.params, Arity: model.Arity{Min: 0, Max: model.Variadic}}}
}

func BuilderCommand(name string) (HookCommand, bool) {
	c, known := commandOf(name)
	return c.public(), known
}

func BuilderCommands() []HookCommand {
	out := make([]HookCommand, len(hookCommands))
	for i, c := range hookCommands {
		out[i] = c.public()
	}
	return out
}

func BuilderFlags(command string) []model.Builtin {
	c, known := commandOf(command)
	if !known {
		return nil
	}
	out := make([]model.Builtin, len(c.flags))
	for i, f := range c.flags {
		out[i] = model.Builtin{Name: f.name, Kind: model.KindAttribute, Signature: f.usage, Doc: f.doc}
	}
	return out
}

const (
	mkDerivationName = "mkDerivation"
	mkShellName      = "mkShell"
)

var (
	derivationKeys = slices.Concat(derivationBindings, []string{
		"pname", "version", "name", "src", "srcs", "sourceRoot", "outputs", metaAttr, "passthru", "patches", "env",
		"buildInputs", "nativeBuildInputs", "propagatedBuildInputs", "propagatedNativeBuildInputs", "checkInputs", "nativeCheckInputs",
		"strictDeps", "enableParallelBuilding", "doCheck", "doInstallCheck", "dontUnpack", "dontPatch", "dontConfigure", "dontBuild", "dontInstall", "dontFixup", "dontStrip",
		"configureFlags", "makeFlags", "buildFlags", "installFlags", "cmakeFlags", "mesonFlags", "__structuredAttrs",
	})
	builderArgumentKeys = map[string][]string{
		mkDerivationName: derivationKeys,
		mkShellName:      {"name", "packages", "inputsFrom", shellHook, "buildInputs", "nativeBuildInputs", "propagatedBuildInputs", "propagatedNativeBuildInputs"},
		shellApp:         {"name", shellAppText, "runtimeInputs", "runtimeEnv", metaAttr, "passthru", "checkPhase", excludeChecks, "extraShellCheckFlags", "derivationArgs", "inheritPath"},
		writeTextFile:    {"pname", "version", "name", shellAppText, "executable", "destination", "checkPhase", metaAttr, "passthru", "allowSubstitutes", "preferLocalBuild", "derivationArgs"},
	}
	metaKeys = []string{descriptionAttr, "longDescription", "homepage", "changelog", "downloadPage", "license", "maintainers", "teams", "platforms", "badPlatforms", "hydraPlatforms", "mainProgram", "broken", "sourceProvenance", "knownVulnerabilities", "priority"}
)

func (c *cursor) builderKeys(self int, bound map[string]bool) []Item {
	keys, owner := c.keysOfSet(self)
	out := make([]Item, 0, len(keys))
	for _, key := range keys {
		if !bound[key] {
			out = append(out, Item{Label: key, Kind: model.KindAttribute, Detail: "argument of `" + owner + "`"})
		}
	}
	return out
}

func (c *cursor) keysOfSet(self int) ([]string, string) {
	if self < 1 {
		return nil, ""
	}
	if assign, assigned := c.chain[self-1].(*syntax.Assign); assigned {
		path, static := attrNames(assign.Path)
		if !static || len(path) != 1 || path[0] != metaAttr || !c.insideBuilderArgument(self-1) {
			return nil, ""
		}
		return metaKeys, metaAttr
	}
	name, ok := c.builderCallAbove(self)
	if !ok {
		return nil, ""
	}
	return builderArgumentKeys[name], name
}

func (c *cursor) insideBuilderArgument(from int) bool {
	for i := from - 1; i >= 0; i-- {
		if _, isSet := c.chain[i].(*syntax.AttrSet); isSet {
			_, ok := c.builderCallAbove(i)
			return ok
		}
	}
	return false
}

func (c *cursor) builderCallAbove(at int) (string, bool) {
	for i := at - 1; i >= 0; i-- {
		switch parent := c.chain[i].(type) {
		case *syntax.Paren, *syntax.Lambda:
		case *syntax.Apply:
			if definedHere(c.table, syntax.Unparenthesized(parent.Fn)) {
				return "", false
			}
			name := calleeName(syntax.Unparenthesized(parent.Fn))
			_, known := builderArgumentKeys[name]
			return name, known
		default:
			return "", false
		}
	}
	return "", false
}
