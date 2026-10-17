package makefile

import (
	"strings"

	"github.com/stubbedev/shell-lsp/internal/host/model"
)

type functionSpec struct {
	name    string
	params  string
	minArgs int
	doc     string
}

var functionSpecs = []functionSpec{
	{"abspath", "names", 1, "The absolute form of each file name, without resolving symbolic links."},
	{"addprefix", "prefix,names", 2, "Each word with the prefix put before it."},
	{"addsuffix", "suffix,names", 2, "Each word with the suffix added after it."},
	{"and", "condition,...", 1, "The last argument when every argument expands to something non-empty, otherwise nothing."},
	{"basename", "names", 1, "Each file name without its suffix."},
	{"call", "variable,param,...", 1, "Expand a variable with `$(1)`, `$(2)` and so on set to the arguments."},
	{"dir", "names", 1, "The directory part of each file name, up to and including the last slash."},
	{"error", "text", 1, "Stop make with the text as a fatal error."},
	{"eval", "text", 1, "Read the text as makefile syntax."},
	{"file", "op filename,text", 1, "Write or append the text to a file, or read the file."},
	{"filter", "patterns,text", 2, "The words that match any of the `%` patterns."},
	{"filter-out", "patterns,text", 2, "The words that match none of the `%` patterns."},
	{"findstring", "find,text", 2, "The first argument when the text contains it, otherwise nothing."},
	{"firstword", "names", 1, "The first word."},
	{"flavor", "variable", 1, "How a variable is defined: `undefined`, `recursive` or `simple`."},
	{"foreach", "var,list,text", 3, "The text expanded once for each word of the list, with the variable set to that word."},
	{"guile", "expression", 1, "The result of evaluating a GNU Guile expression, when make is built with Guile."},
	{"if", "condition,then,else", 2, "The second argument when the condition expands to something non-empty, otherwise the third."},
	{"info", "text", 1, "Print the text."},
	{"intcmp", "lhs,rhs,lt,eq,gt", 2, "Compare two integers and expand the argument that matches the result."},
	{"join", "list1,list2", 2, "The words of both lists joined pairwise."},
	{"lastword", "names", 1, "The last word."},
	{"let", "vars,list,text", 3, "The text expanded with the variables bound to the words of the list."},
	{"notdir", "names", 1, "Each file name without its directory part."},
	{"or", "condition,...", 1, "The first argument that expands to something non-empty."},
	{"origin", "variable", 1, "Where a variable was defined, such as `file`, `environment` or `undefined`."},
	{"patsubst", "pattern,replacement,text", 3, "The words that match the `%` pattern replaced by the replacement."},
	{"realpath", "names", 1, "The canonical absolute form of each existing file name."},
	{"shell", "command", 1, "The output of running the command in the shell, with newlines turned into spaces."},
	{"sort", "list", 1, "The words in lexical order, without duplicates."},
	{"strip", "string", 1, "The text with leading and trailing whitespace removed and inner runs collapsed to one space."},
	{"subst", "from,to,text", 3, "The text with every occurrence of `from` replaced by `to`."},
	{"suffix", "names", 1, "The suffix of each file name."},
	{"value", "variable", 1, "The value of a variable without expanding it."},
	{"warning", "text", 1, "Print the text as a warning."},
	{"wildcard", "pattern", 1, "The existing file names that match the shell wildcard patterns."},
	{"word", "n,text", 2, "The nth word, counting from 1."},
	{"wordlist", "s,e,text", 3, "The words from position s to position e."},
	{"words", "text", 1, "The number of words."},
}

var automaticVariables = []model.Builtin{
	{Name: "@", Doc: "The file name of the target of the rule."},
	{Name: "%", Doc: "The member name when the target is an archive member."},
	{Name: "<", Doc: "The name of the first prerequisite."},
	{Name: "?", Doc: "The prerequisites that are newer than the target."},
	{Name: "^", Doc: "All prerequisites, without duplicates."},
	{Name: "+", Doc: "All prerequisites, duplicates included, in order."},
	{Name: "|", Doc: "The order-only prerequisites."},
	{Name: "*", Doc: "The stem that matched `%` in an implicit or static pattern rule."},
}

var specialVariables = map[string]string{
	"AR": "The archive-maintaining program; `ar` by default.", "ARFLAGS": "Flags for the archive program.",
	"AS": "The assembler; `as` by default.", "ASFLAGS": "Extra flags for the assembler.",
	"CC": "The C compiler; `cc` by default.", "CFLAGS": "Extra flags for the C compiler.",
	"CO": "The RCS checkout program.", "COFLAGS": "Extra flags for the RCS checkout program.",
	"CPP": "The C preprocessor.", "CPPFLAGS": "Extra flags for the C preprocessor and the programs that use it.",
	"CTANGLE": "The program that turns CWEB into C.", "CURDIR": "The directory make is running in.",
	"CWEAVE": "The program that turns CWEB into TeX.", "CXX": "The C++ compiler; `g++` by default.",
	"CXXFLAGS": "Extra flags for the C++ compiler.", "F77": "The Fortran 77 compiler.",
	"FC": "The Fortran compiler.", "FFLAGS": "Extra flags for the Fortran compiler.",
	"GET": "The SCCS get program.", "GFLAGS": "Extra flags for the SCCS get program.",
	"GNUMAKEFLAGS": "Flags make reads before `MAKEFLAGS`.", "LD": "The linker.",
	"LDFLAGS": "Extra flags for compilers that call the linker.", "LDLIBS": "Libraries for compilers that call the linker.",
	"LEX": "The program that turns Lex grammars into C.", "LFLAGS": "Extra flags for Lex.",
	"LINT": "The lint program.", "LINTFLAGS": "Extra flags for lint.", "LOADLIBES": "Old name for `LDLIBS`.",
	"M2C": "The Modula-2 compiler.", "MAKE": "The name make was started with; use it for recursive make calls.",
	"MAKECMDGOALS": "The targets given on the command line.", "MAKEFILES": "Makefiles to read before the others.",
	"MAKEFILE_LIST": "The makefiles read so far.", "MAKEFLAGS": "The flags of make, passed to recursive calls.",
	"MAKEINFO": "The program that turns Texinfo into Info.", "MAKELEVEL": "The depth of recursive make calls.",
	"MAKEOVERRIDES": "The variable definitions given on the command line.", "MAKESHELL": "The shell on MS-DOS.",
	"MAKE_COMMAND": "The command make was started with.", "MAKE_HOST": "The host make was built for.",
	"MAKE_RESTARTS": "How often make has restarted itself.", "MAKE_TERMERR": "Whether standard error is a terminal.",
	"MAKE_TERMOUT": "Whether standard output is a terminal.", "MAKE_VERSION": "The version of make.",
	"OBJC": "The Objective-C compiler.", "OUTPUT_OPTION": "The option that names a compiler's output file.",
	"PC": "The Pascal compiler.", "PFLAGS": "Extra flags for the Pascal compiler.",
	"RFLAGS": "Extra flags for the Ratfor compiler.", "RM": "The command that removes files; `rm -f` by default.",
	"SHELL": "The shell that runs recipes; `/bin/sh` by default.", "SUFFIXES": "The default suffix list.",
	"TANGLE": "The program that turns WEB into Pascal.", "TARGET_ARCH": "Architecture flags for compilers.",
	"TEX": "The TeX program.", "TEXI2DVI": "The program that turns Texinfo into DVI.",
	"VPATH": "Directories to search for prerequisites.", "WEAVE": "The program that turns WEB into TeX.",
	"YACC": "The program that turns Yacc grammars into C.", "YFLAGS": "Extra flags for Yacc.",
	".DEFAULT_GOAL": "The target make builds when none is given.", ".EXTRA_PREREQS": "Prerequisites added to every target.",
	".FEATURES": "The features this make supports.", ".INCLUDE_DIRS": "The directories searched for included makefiles.",
	".LIBPATTERNS": "The patterns for library names in prerequisites.", ".LOADED": "The objects loaded with `load`.",
	".RECIPEPREFIX": "The character that starts recipe lines; a tab by default.", ".SHELLFLAGS": "The flags passed to the shell; `-c` by default.",
	".VARIABLES": "The names of every variable defined so far.",
}

var environmentVariables = []string{"HOME", "PATH", "PWD", "USER", "LOGNAME", "TMPDIR", "LANG", "TERM", "DESTDIR", "PREFIX", "OS"}

var specialTargets = map[string]string{
	".DEFAULT":              "The recipe for targets that have no rule.",
	".DELETE_ON_ERROR":      "Delete a target when its recipe fails.",
	".EXPORT_ALL_VARIABLES": "Export every variable to the recipes' environment.",
	".IGNORE":               "Ignore errors in the recipes of its prerequisites, or in every recipe.",
	".INTERMEDIATE":         "Treat its prerequisites as intermediate files.",
	".LOW_RESOLUTION_TIME":  "Compare the timestamps of its prerequisites to the second.",
	".NOTINTERMEDIATE":      "Never treat its prerequisites as intermediate files.",
	".NOTPARALLEL":          "Run recipes one at a time.",
	".ONESHELL":             "Run each recipe in one shell instead of one shell per line.",
	".PHONY":                "Its prerequisites are names of actions, not files, and always run.",
	".POSIX":                "Follow POSIX: recipes run with `-e`, among other changes.",
	".PRECIOUS":             "Keep its prerequisites when make is interrupted or they are intermediate.",
	".SECONDARY":            "Treat its prerequisites as intermediate files that are never deleted.",
	".SECONDEXPANSION":      "Expand prerequisites a second time when the rule runs.",
	".SILENT":               "Do not echo the recipes of its prerequisites, or of every recipe.",
	".SUFFIXES":             "The suffixes of old-fashioned suffix rules.",
	".WAIT":                 "Wait for the prerequisites before it to finish before starting those after it.",
}

var directiveDocs = []model.Builtin{
	{Name: "define", Signature: "define NAME =\n...\nendef", Doc: "A variable whose value spans several lines."},
	{Name: "else", Signature: "else", Doc: "The part of a conditional read when its condition is false."},
	{Name: "endef", Signature: "endef", Doc: "Ends a `define`."},
	{Name: "endif", Signature: "endif", Doc: "Ends a conditional."},
	{Name: "export", Signature: "export NAME", Doc: "Pass a variable to recipes as an environment variable; alone, export every variable."},
	{Name: "ifdef", Signature: "ifdef NAME", Doc: "Read what follows when the variable has a non-empty value."},
	{Name: "ifeq", Signature: "ifeq (a,b)", Doc: "Read what follows when both arguments expand to the same text."},
	{Name: "ifndef", Signature: "ifndef NAME", Doc: "Read what follows when the variable is empty or undefined."},
	{Name: "ifneq", Signature: "ifneq (a,b)", Doc: "Read what follows when the arguments expand to different text."},
	{Name: "include", Signature: "include FILE...", Doc: "Read other makefiles here; a missing one is an error."},
	{Name: "-include", Signature: "-include FILE...", Doc: "Read other makefiles here, ignoring missing ones."},
	{Name: "load", Signature: "load OBJECT", Doc: "Load a dynamic object that adds functions to make."},
	{Name: "override", Signature: "override NAME = VALUE", Doc: "Set a variable even when it was given on the command line."},
	{Name: "private", Signature: "private NAME = VALUE", Doc: "A target-specific variable that prerequisites do not inherit."},
	{Name: "sinclude", Signature: "sinclude FILE...", Doc: "Read other makefiles here, ignoring missing ones."},
	{Name: "undefine", Signature: "undefine NAME", Doc: "Make a variable undefined."},
	{Name: "unexport", Signature: "unexport NAME", Doc: "Stop passing a variable to recipes."},
	{Name: "vpath", Signature: "vpath PATTERN DIRS", Doc: "Directories to search for prerequisites that match a pattern."},
}

func builtinFunction(name string) bool {
	_, ok := Builtins.Lookup(model.KindFunction, name)
	return ok
}

var Builtins = func() model.Builtins {
	var entries []model.Builtin
	for _, f := range functionSpecs {
		entries = append(entries, model.Builtin{Name: f.name, Kind: model.KindFunction, Signature: "$(" + f.name + " " + f.params + ")", Params: strings.Split(f.params, ","), Doc: f.doc, Arity: model.Arity{Min: f.minArgs, Max: model.Variadic}})
	}
	for _, v := range automaticVariables {
		v.Kind, v.Signature, v.Arity = model.KindVariable, "$"+v.Name, model.NoArity
		entries = append(entries, v)
		for suffix, part := range map[string]string{"D": "directory part", "F": "file name part"} {
			entries = append(entries, model.Builtin{Name: v.Name + suffix, Kind: model.KindVariable, Signature: "$(" + v.Name + suffix + ")", Doc: "The " + part + " of `$" + v.Name + "`.", Arity: model.NoArity})
		}
	}
	for name, doc := range specialVariables {
		entries = append(entries, model.Builtin{Name: name, Kind: model.KindVariable, Signature: "$(" + name + ")", Doc: doc, Arity: model.NoArity})
	}
	for _, name := range environmentVariables {
		entries = append(entries, model.Builtin{Name: name, Kind: model.KindVariable, Signature: "$(" + name + ")", Doc: "Usually set in the environment, which make reads as variables.", Arity: model.NoArity})
	}
	for name, doc := range specialTargets {
		entries = append(entries, model.Builtin{Name: name, Kind: model.KindTarget, Signature: name + ":", Doc: doc, Arity: model.NoArity})
	}
	for _, d := range directiveDocs {
		d.Kind, d.Arity = model.KindKeyword, model.NoArity
		entries = append(entries, d)
	}
	return model.NewBuiltins(entries)
}()
