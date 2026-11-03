package nix

import (
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

var phaseStages = []string{"Unpack", "Patch", "Configure", "Build", "Check", "Install", "Fixup", "InstallCheck", "Dist"}

const (
	shellHookRank = 1
	firstStage    = 2
	stageSteps    = 3
)

func phaseRank(binding string) int {
	if binding == shellHook {
		return shellHookRank
	}
	for i, stage := range phaseStages {
		base := firstStage + stageSteps*i
		switch binding {
		case "pre" + stage:
			return base
		case stageBinding(stage):
			return base + 1
		case "post" + stage:
			return base + 2
		}
	}
	return firstStage + stageSteps*len(phaseStages)
}

var stdenvReads = stdenvReadSet()

func stdenvReadSet() map[string]bool {
	reads := make(map[string]bool, len(stdenvInputs)+len(derivationBindings)+2*len(phaseStages))
	for _, names := range [][]string{stdenvInputs, derivationBindings} {
		for _, name := range names {
			reads[name] = true
		}
	}
	for _, stage := range phaseStages {
		reads["pre"+stage+"Hooks"] = true
		reads["post"+stage+"Hooks"] = true
	}
	return reads
}

var stdenvInputs = []string{
	"NIX_BUILD_CORES", "NIX_BUILD_TOP", "NIX_DEBUG", "SOURCE_DATE_EPOCH", "TMPDIR",
	"buildFlags", "buildFlagsArray",
	"checkFlags", "checkFlagsArray", "checkTarget",
	"configureFlags", "configureFlagsArray", "configureScript",
	"depsBuildBuildPropagated", "depsBuildTargetPropagated", "depsHostHostPropagated", "depsTargetTargetPropagated",
	"distFlags", "distFlagsArray", "distTarget",
	"doCheck", "doDist", "doInstallCheck",
	"dontAddDisableDepTrack", "dontAddPrefix", "dontBuild", "dontCheckForBrokenSymlinks", "dontConfigure", "dontCopyDist",
	"dontDisableStatic", "dontFixLibtool", "dontFixup", "dontInstall", "dontMakeSourcesWritable", "dontMoveLib64", "dontMoveSbin",
	"dontMoveSystemdUserUnits", "dontPatch", "dontPatchShebangs", "dontPatchShebangsInConfigure", "dontPruneLibtoolFiles",
	"dontStrip", "dontUnpack",
	"enableParallelBuilding", "enableParallelChecking", "enableParallelInstalling",
	"exitHook", "exitHooks", "failureHook", "failureHooks", "fixupOutput", "fixupOutputHooks",
	"forceShare", "foundMakefile",
	"installCheckFlags", "installCheckFlagsArray", "installCheckTarget",
	"installFlags", "installFlagsArray", "installTargets",
	"makeFlags", "makeFlagsArray", "makefile", "moveToDev", "name", "noDumpEnvVars", "out",
	"outputBin", "outputDev", "outputDevdoc", "outputDevman", "outputDoc", "outputInclude", "outputInfo", "outputLib", "outputMan", "outputs",
	"patchFlags", "patches", "prefix", "prefixKey",
	"propagatedBuildInputs", "propagatedBuildOutputs", "propagatedNativeBuildInputs", "propagatedUserEnvPkgs",
	"setOutputFlags", "setSourceRoot", "setupHook", "setupHooks", "showBuildStats", "sourceRoot", "src", "srcs", "strictDeps",
	"stripAllFlags", "stripAllList", "stripAllListTarget", "stripDebugFlags", "stripDebugList", "stripDebugListTarget", "stripExclude",
	"succeedOnFailure", "tarballs", "unpackCmd", "unpackCmdHooks",
}

const spacelessValue = "=x\n"

func (f *file) spacelessPrelude(group []shellString) string {
	first := group[0]
	if !first.derivation && len(first.provides) == 0 {
		return ""
	}
	known := func(name string) bool {
		return first.derivation && spacelessBuilderVariables[name] || slices.Contains(first.provides, name)
	}
	var names []string
	for _, s := range group {
		names = f.spacelessReads(s.str, known, names)
	}
	slices.Sort(names)
	var sb strings.Builder
	for _, name := range slices.Compact(names) {
		sb.WriteString(name)
		sb.WriteString(spacelessValue)
	}
	return sb.String()
}

func (f *file) spacelessReads(str *syntax.String, known func(string) bool, names []string) []string {
	for i, part := range str.Parts {
		switch part := part.(type) {
		case *syntax.Literal:
			names = appendExpansions(part.Text, known, names)
		case *syntax.Escape:
			if !strings.HasSuffix(part.Text, "$") && !strings.HasSuffix(part.Text, "${") || i+1 == len(str.Parts) {
				continue
			}
			if next, isLiteral := str.Parts[i+1].(*syntax.Literal); isLiteral {
				names = appendName(next.Text, 0, known, names)
			}
		case *syntax.Interpolation:
			sp, spliced := f.holeSplice(part)
			if !spliced {
				continue
			}
			for _, item := range sp.items {
				if nested, isString := item.(*syntax.String); isString {
					names = f.spacelessReads(nested, known, names)
				}
			}
		}
	}
	return names
}

func appendExpansions(text string, known func(string) bool, names []string) []string {
	for at := strings.IndexByte(text, '$'); at >= 0; {
		names = appendName(text, at+1, known, names)
		next := strings.IndexByte(text[at+1:], '$')
		if next < 0 {
			break
		}
		at += 1 + next
	}
	return names
}

func appendName(text string, start int, known func(string) bool, names []string) []string {
	if start < len(text) && text[start] == '{' {
		start++
	}
	end := start
	for end < len(text) && isNameByte(text[end]) {
		end++
	}
	if name := text[start:end]; name != "" && known(name) {
		names = append(names, name)
	}
	return names
}

func isNameByte(c byte) bool {
	return c == '_' || isASCIIAlnum(rune(c))
}

var spacelessBuilderVariables = map[string]bool{
	"CONFIG_SHELL":      true,
	"NIX_BUILD_CORES":   true,
	"NIX_BUILD_TOP":     true,
	"NIX_LOG_FD":        true,
	"NIX_STORE":         true,
	"SHELL":             true,
	"SOURCE_DATE_EPOCH": true,
	"TEMP":              true,
	"TEMPDIR":           true,
	"TMP":               true,
	"TMPDIR":            true,
	"TZ":                true,
	"builder":           true,
	"name":              true,
	"out":               true,
	"pname":             true,
	"stdenv":            true,
	"system":            true,
	"version":           true,
}

func declaredOutputs(set *syntax.AttrSet) []string {
	if set == nil {
		return nil
	}
	for _, b := range set.Bindings {
		a, assigned := b.(*syntax.Assign)
		if !assigned || len(a.Path) != 1 || lastIdent(a.Path) != "outputs" {
			continue
		}
		list, isList := syntax.Unparenthesized(a.Value).(*syntax.List)
		if !isList {
			return nil
		}
		var names []string
		for _, item := range list.Items {
			if text, isString := syntax.Unparenthesized(item).(*syntax.String); isString {
				if name := literalOf(text); parser.IsName(name) {
					names = append(names, name)
				}
			}
		}
		return names
	}
	return nil
}

var builderVariables = map[string]string{
	"CONFIG_SHELL":      "The shell `configure` scripts run with; `setup.sh` exports it.",
	"HOME":              "The builder's home, which the Nix builder points at a path that does not exist.",
	"NIX_BUILD_CORES":   "How many cores a build step may use; `setup.sh` exports it.",
	"NIX_BUILD_TOP":     "The temporary directory the build runs in, set by the Nix builder.",
	"NIX_LOG_FD":        "The file descriptor of the build log, set by the Nix builder.",
	"NIX_STORE":         "The Nix store directory, set by the Nix builder.",
	"PATH":              "The search path, assembled by `setup.sh` from the build inputs.",
	"PWD":               "The working directory, which starts as the build directory.",
	"SHELL":             "The shell that runs the phases; `setup.sh` exports it.",
	"SOURCE_DATE_EPOCH": "The timestamp reproducible builds use; `setup.sh` exports it.",
	"TEMP":              "The temporary directory, set by the Nix builder.",
	"TEMPDIR":           "The temporary directory, set by the Nix builder.",
	"TMP":               "The temporary directory, set by the Nix builder.",
	"TMPDIR":            "The temporary directory, set by the Nix builder.",
	"TZ":                "The time zone; `setup.sh` exports `UTC`.",
	"builder":           "The program that runs the build, an attribute of every derivation.",
	"curPhase":          "The name of the phase that is running, set by `setup.sh`.",
	"name":              "The name of the derivation, an attribute of every derivation.",
	"out":               "The store path of the default output, set by the Nix builder.",
	"outputs":           "The names of the outputs; `setup.sh` defaults it to `out`.",
	"phases":            "The phases to run; `setup.sh` fills in the default list.",
	"pname":             "The package name, an attribute `mkDerivation` takes.",
	"prefix":            "The install prefix; `setup.sh` defaults it to `$out`.",
	"sourceRoot":        "The directory unpacking left the sources in, set by `setup.sh`.",
	"src":               "The source to unpack, an attribute `mkDerivation` takes.",
	"srcs":              "The sources to unpack, an attribute `mkDerivation` takes.",
	"stdenv":            "The standard environment of the derivation, whose `setup` script runs the phases.",
	"system":            "The platform of the build, an attribute of every derivation.",
	"version":           "The package version, an attribute `mkDerivation` takes.",
}

func BuilderVariable(name string) (model.Builtin, bool) {
	doc, known := builderVariables[name]
	if !known {
		return model.Builtin{}, false
	}
	return model.Builtin{Name: name, Kind: model.KindVariable, Signature: "$" + name, Doc: doc}, true
}

func PhaseName(rank int) string {
	if rank == shellHookRank {
		return shellHook
	}
	i := slices.IndexFunc(derivationBindings, func(binding string) bool { return phaseRank(binding) == rank })
	if i < 0 {
		return "a phase"
	}
	return derivationBindings[i]
}

func (f *file) holeReads(group []shellString) string {
	var names []string
	collect := func(n syntax.Node) bool {
		if literal, isLiteral := n.(*syntax.Literal); isLiteral {
			names = appendExpansions(literal.Text, func(string) bool { return true }, names)
		}
		return true
	}
	for _, s := range group {
		f.eachHole(s.str, func(hole *syntax.Interpolation) { syntax.Inspect(hole.Value, collect) })
	}
	if len(names) == 0 {
		return ""
	}
	slices.Sort(names)
	var sb strings.Builder
	for _, name := range slices.Compact(names) {
		sb.WriteString("\n: \"$")
		sb.WriteString(name)
		sb.WriteByte('"')
	}
	return sb.String()
}
