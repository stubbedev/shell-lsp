package nix

import (
	"slices"

	"github.com/stubbedev/shell-lsp/internal/host/model"
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
