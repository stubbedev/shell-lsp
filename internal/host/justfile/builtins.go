package justfile

import "github.com/stubbedev/shell-lsp/internal/host/model"

type settingType uint8

const (
	settingBool settingType = iota
	settingString
	settingList
)

type settingSpec struct {
	name       string
	kind       settingType
	doc        string
	deprecated string
}

var settingSpecs = []settingSpec{
	{"allow-duplicate-recipes", settingBool, "Let a later recipe with the same name replace an earlier one instead of failing.", ""},
	{"allow-duplicate-variables", settingBool, "Let a later assignment to the same variable replace an earlier one instead of failing.", ""},
	{"default-list", settingBool, "Make plain `just` list the recipes instead of running the default recipe.", ""},
	{"default-script", settingBool, "Run every recipe as a script, as if it had a `[script]` attribute.", ""},
	{"dotenv-command", settingString, "Run this command and read its output as environment variable assignments.", ""},
	{"dotenv-filename", settingString, "Load environment variables from a file with this name, searching upwards, when one exists.", ""},
	{"dotenv-load", settingBool, "Load environment variables from a `.env` file when one exists.", ""},
	{"dotenv-override", settingBool, "Let values from the environment file replace variables that are already set.", ""},
	{"dotenv-path", settingString, "Load environment variables from this file, and fail when it is missing.", ""},
	{"dotenv-required", settingBool, "Fail when no environment file is found.", ""},
	{"export", settingBool, "Pass every variable and parameter to recipes as an environment variable.", ""},
	{"fallback", settingBool, "Look for a justfile in the parent directory when a recipe on the command line is not found here.", ""},
	{"guards", settingBool, "Treat a leading `?` on a recipe line as a guard: exit status 1 stops the recipe without failing.", ""},
	{"ignore-comments", settingBool, "Skip recipe lines that start with `#` instead of passing them to the shell.", ""},
	{"indentation", settingString, "The indentation `just --fmt` writes for recipe bodies.", ""},
	{"lazy", settingBool, "Evaluate a variable only when something uses it.", ""},
	{"lists", settingBool, "Let values be lists of strings. Unstable.", ""},
	{"minimum-version", settingString, "Fail when `just` is older than this version, written as MAJOR.MINOR.PATCH.", ""},
	{"no-cd", settingBool, "Run recipes in the directory `just` was started in instead of the justfile's directory.", ""},
	{"no-exit-message", settingBool, "Do not print a message when a recipe fails.", ""},
	{"positional-arguments", settingBool, "Pass recipe arguments to the shell as positional parameters `$1`, `$2` and so on.", ""},
	{"quiet", settingBool, "Do not echo recipe lines before running them.", ""},
	{"script-interpreter", settingList, "The command and arguments that run recipes with a bare `[script]` attribute. Defaults to `['sh', '-eu']`.", ""},
	{"shell", settingList, "The command and arguments that run recipe lines and backticks. Defaults to `['sh', '-cu']`.", ""},
	{"tempdir", settingString, "Create the temporary files for script recipes in this directory.", ""},
	{"unstable", settingBool, "Allow features that are not stable yet.", ""},
	{"windows-powershell", settingBool, "Use PowerShell as the shell on Windows.", "use `[windows]` on `set shell` instead"},
	{"windows-shell", settingList, "The shell used on Windows.", "use `[windows]` on `set shell` instead"},
	{"working-directory", settingString, "Run recipes and backticks in this directory, relative to the justfile.", ""},
}

type attributeSpec struct {
	name     string
	arity    model.Arity
	doc      string
	literals []string
}

const positionalArgument = ""

var keyedAttributes = []string{"arg", "cache"}

var attributeSpecs = []attributeSpec{
	{"android", args(0, 0), "Enable the item only on Android.", nil},
	{"arg", args(1, model.Variadic), "Configure how a parameter is passed on the command line: `long`, `short`, `value`, `flag`, `multiple`, `help`, `pattern`, `min` and `max`.", []string{positionalArgument, "long", "short"}},
	{"cache", args(0, model.Variadic), "Skip the recipe when its cached inputs and outputs are unchanged. Unstable.", nil},
	{"confirm", args(0, 1), "Ask for confirmation before running the recipe, optionally with a custom prompt.", nil},
	{"continue", args(0, 1), "Keep running when a command is interrupted by one of these signals and then exits successfully.", []string{positionalArgument}},
	{"default", args(0, 0), "Make this the module's default recipe.", nil},
	{"doc", args(0, 1), "Set the documentation shown by `just --list`, or remove it when given no argument.", nil},
	{"dragonfly", args(0, 0), "Enable the item only on DragonFly BSD.", nil},
	{"env", args(2, 2), "Set an environment variable for the recipe.", nil},
	{"exit-message", args(0, 0), "Print a message when the recipe fails, even with `set no-exit-message`.", nil},
	{"extension", args(1, 1), "The file extension of the temporary script file of a shebang or script recipe.", []string{positionalArgument}},
	{"freebsd", args(0, 0), "Enable the item only on FreeBSD.", nil},
	{"group", args(1, 1), "Put the recipe or module in a named group in `just --list`.", []string{positionalArgument}},
	{"linux", args(0, 0), "Enable the item only on Linux.", nil},
	{"macos", args(0, 0), "Enable the item only on macOS.", nil},
	{"metadata", args(1, model.Variadic), "Attach metadata that `just --dump` reports.", []string{positionalArgument}},
	{"netbsd", args(0, 0), "Enable the item only on NetBSD.", nil},
	{"no-cd", args(0, 0), "Run the recipe in the directory `just` was started in.", nil},
	{"no-exit-message", args(0, 0), "Do not print a message when the recipe fails.", nil},
	{"no-quiet", args(0, 0), "Echo the recipe's lines even when quiet is set.", nil},
	{"openbsd", args(0, 0), "Enable the item only on OpenBSD.", nil},
	{"parallel", args(0, 0), "Run the recipe's dependencies in parallel.", nil},
	{"positional-arguments", args(0, 0), "Pass the recipe's arguments to the shell as positional parameters.", nil},
	{"private", args(0, 0), "Hide the item from `just --list`.", nil},
	{"script", args(0, model.Variadic), "Run the body as one script, with the given interpreter or `script-interpreter`.", []string{positionalArgument}},
	{"shell", args(0, 0), "Run the recipe line by line in the shell, overriding `set default-script`.", nil},
	{"timestamp", args(0, 1), "Print a timestamp before each command, optionally in a given format.", nil},
	{"unix", args(0, 0), "Enable the item only on Unix systems, macOS included.", nil},
	{"windows", args(0, 0), "Enable the item only on Windows.", nil},
	{"working-directory", args(1, 1), "Run the recipe in this directory, relative to the default one.", nil},
}

type functionSpec struct {
	name       string
	params     string
	arity      model.Arity
	doc        string
	deprecated string
}

var functionSpecs = []functionSpec{
	{"absolute_path", "path", args(1, 1), "The absolute form of a path relative to the working directory.", ""},
	{"append", "suffix, s", args(2, 2), "Add a suffix to every whitespace-separated word.", ""},
	{"arch", "", args(0, 0), "The processor architecture, such as `x86_64` or `aarch64`.", ""},
	{"assert", "condition, message", args(1, 2), "Stop with the message when the condition is false.", ""},
	{"blake3", "s", args(1, 1), "The BLAKE3 hash of a string, in hexadecimal.", ""},
	{"blake3_file", "path", args(1, 1), "The BLAKE3 hash of a file's contents, in hexadecimal.", ""},
	{"cache_directory", "", args(0, 0), "The user's cache directory.", ""},
	{"canonicalize", "path", args(1, 1), "A path with symbolic links, `.` and `..` resolved.", ""},
	{"capitalize", "s", args(1, 1), "The string with its first character upper case and the rest lower case.", ""},
	{"choose", "n, alphabet", args(2, 2), "A random string of n characters taken from the alphabet.", ""},
	{"clean", "path", args(1, 1), "A path with redundant separators, `.` and resolvable `..` removed.", ""},
	{"config_directory", "", args(0, 0), "The user's configuration directory.", ""},
	{"config_local_directory", "", args(0, 0), "The user's local configuration directory.", ""},
	{"data_directory", "", args(0, 0), "The user's data directory.", ""},
	{"data_local_directory", "", args(0, 0), "The user's local data directory.", ""},
	{"datetime", "format", args(1, 1), "The local time, formatted with strftime directives.", ""},
	{"datetime_utc", "format", args(1, 1), "The time in UTC, formatted with strftime directives.", ""},
	{"encode_uri_component", "s", args(1, 1), "The string with every character outside `A-Za-z0-9_.!~*'()-` percent-encoded.", ""},
	{"env", "key, default", args(1, 2), "The value of an environment variable; without a default, a missing variable is an error.", ""},
	{"env_var", "key", args(1, 1), "The value of an environment variable.", "use `env(key)`"},
	{"env_var_or_default", "key, default", args(2, 2), "The value of an environment variable, or the default when it is not set.", "use `env(key, default)`"},
	{"error", "message", args(1, 1), "Stop with an error message.", ""},
	{"executable_directory", "", args(0, 0), "The user's directory for executables.", ""},
	{"extension", "path", args(1, 1), "The extension of a path's file name.", ""},
	{"file_name", "path", args(1, 1), "The last component of a path.", ""},
	{"file_stem", "path", args(1, 1), "The file name of a path without its extension.", ""},
	{"home_directory", "", args(0, 0), "The user's home directory.", ""},
	{"invocation_directory", "", args(0, 0), "The directory `just` was started in, converted to a Cygwin path on Windows.", ""},
	{"invocation_directory_native", "", args(0, 0), "The directory `just` was started in, as the system writes it.", ""},
	{"is_dependency", "", args(0, 0), "`true` when the current recipe runs as a dependency, otherwise `false`.", ""},
	{"join", "a, b, ...", args(2, model.Variadic), "Paths joined with the system's separator.", ""},
	{"join_list", "list", args(1, 1), "The elements of a list joined with spaces.", ""},
	{"just_executable", "", args(0, 0), "The path of the running `just`.", ""},
	{"just_pid", "", args(0, 0), "The process ID of the running `just`.", ""},
	{"just_version", "", args(0, 0), "The version of the running `just`.", ""},
	{"justfile", "", args(0, 0), "The path of the root justfile.", ""},
	{"justfile_directory", "", args(0, 0), "The directory of the root justfile.", ""},
	{"kebabcase", "s", args(1, 1), "The string in kebab-case.", ""},
	{"lowercamelcase", "s", args(1, 1), "The string in lowerCamelCase.", ""},
	{"lowercase", "s", args(1, 1), "The string in lower case.", ""},
	{"module_directory", "", args(0, 0), "The directory of the current module's source file.", ""},
	{"module_file", "", args(0, 0), "The path of the current module's source file.", ""},
	{"module_path", "", args(0, 0), "The `::`-separated path of the current module.", ""},
	{"num_cpus", "", args(0, 0), "The number of logical processors.", ""},
	{"num_jobs", "", args(0, 0), "The value of `--jobs`, or an empty list when it was not given.", ""},
	{"os", "", args(0, 0), "The operating system, such as `linux`, `macos` or `windows`.", ""},
	{"os_family", "", args(0, 0), "`unix` or `windows`.", ""},
	{"parent_directory", "path", args(1, 1), "A path without its last component.", ""},
	{"path_exists", "path", args(1, 1), "`true` when the path exists, following symbolic links, otherwise `false`.", ""},
	{"prepend", "prefix, s", args(2, 2), "Put a prefix before every whitespace-separated word.", ""},
	{"quote", "s", args(1, 1), "The string in single quotes, with its own single quotes escaped for the shell.", ""},
	{"read", "path", args(1, 1), "The contents of a file.", ""},
	{"recipe_name", "", args(0, 0), "The name of the current recipe.", ""},
	{"replace", "s, from, to", args(3, 3), "The string with every occurrence of `from` replaced by `to`.", ""},
	{"replace_regex", "s, regex, replacement", args(3, 3), "The string with every match of a regular expression replaced; `$1` and `${name}` refer to groups.", ""},
	{"require", "name", args(1, 1), "The full path of an executable found on `PATH`; a missing one is an error.", ""},
	{"runtime_directory", "", args(0, 0), "The user's runtime directory, on Linux only.", ""},
	{"semver_matches", "version, requirement", args(2, 2), "`true` when a semantic version satisfies a requirement such as `>=1.2`.", ""},
	{"sha256", "s", args(1, 1), "The SHA-256 hash of a string, in hexadecimal.", ""},
	{"sha256_file", "path", args(1, 1), "The SHA-256 hash of a file's contents, in hexadecimal.", ""},
	{"shell", "command, args...", args(1, model.Variadic), "The output of a command run by the configured shell, with the arguments as positional parameters.", ""},
	{"shoutykebabcase", "s", args(1, 1), "The string in SHOUTY-KEBAB-CASE.", ""},
	{"shoutysnakecase", "s", args(1, 1), "The string in SHOUTY_SNAKE_CASE.", ""},
	{"snakecase", "s", args(1, 1), "The string in snake_case.", ""},
	{"source_directory", "", args(0, 0), "The directory of the file this expression is written in.", ""},
	{"source_file", "", args(0, 0), "The path of the file this expression is written in.", ""},
	{"style", "styles, text", args(1, 2), "A terminal escape sequence for named styles and colours, or the text wrapped in it.", ""},
	{"titlecase", "s", args(1, 1), "The string in Title Case.", ""},
	{"trim", "s", args(1, 1), "The string without leading and trailing whitespace.", ""},
	{"trim_end", "s", args(1, 1), "The string without trailing whitespace.", ""},
	{"trim_end_match", "s, suffix", args(2, 2), "The string without one copy of a suffix.", ""},
	{"trim_end_matches", "s, suffix", args(2, 2), "The string without every repeated copy of a suffix.", ""},
	{"trim_start", "s", args(1, 1), "The string without leading whitespace.", ""},
	{"trim_start_match", "s, prefix", args(2, 2), "The string without one copy of a prefix.", ""},
	{"trim_start_matches", "s, prefix", args(2, 2), "The string without every repeated copy of a prefix.", ""},
	{"uppercamelcase", "s", args(1, 1), "The string in UpperCamelCase.", ""},
	{"uppercase", "s", args(1, 1), "The string in upper case.", ""},
	{"uuid", "", args(0, 0), "A random version 4 UUID.", ""},
	{"which", "name", args(1, 1), "The full path of an executable found on `PATH`, or an empty list. Needs `set lists`.", ""},
	{"without_extension", "path", args(1, 1), "A path without the extension of its file name.", ""},
}

type constantSpec struct {
	name, value string
}

var constantSpecs = []constantSpec{
	{"BG_BLACK", `"\e[40m"`},
	{"BG_BLUE", `"\e[44m"`},
	{"BG_CYAN", `"\e[46m"`},
	{"BG_GREEN", `"\e[42m"`},
	{"BG_MAGENTA", `"\e[45m"`},
	{"BG_RED", `"\e[41m"`},
	{"BG_WHITE", `"\e[47m"`},
	{"BG_YELLOW", `"\e[43m"`},
	{"BLACK", `"\e[30m"`},
	{"BLUE", `"\e[34m"`},
	{"BOLD", `"\e[1m"`},
	{"CLEAR", `"\ec"`},
	{"CYAN", `"\e[36m"`},
	{"GREEN", `"\e[32m"`},
	{"HEX", `"0123456789abcdef"`},
	{"HEXLOWER", `"0123456789abcdef"`},
	{"HEXUPPER", `"0123456789ABCDEF"`},
	{"HIDE", `"\e[8m"`},
	{"INVERT", `"\e[7m"`},
	{"ITALIC", `"\e[3m"`},
	{"MAGENTA", `"\e[35m"`},
	{"NORMAL", `"\e[0m"`},
	{"PATH_SEP", `"/" ("\" on Windows)`},
	{"PATH_VAR_SEP", `":" (";" on Windows)`},
	{"RED", `"\e[31m"`},
	{"STRIKETHROUGH", `"\e[9m"`},
	{"UNDERLINE", `"\e[4m"`},
	{"WHITE", `"\e[37m"`},
	{"YELLOW", `"\e[33m"`},
}

var keywordDocs = []model.Builtin{
	{Name: "alias", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "alias NAME := RECIPE", Doc: "Another name for a recipe."},
	{Name: "eager", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "eager NAME := VALUE", Doc: "An assignment evaluated even when `set lazy` is on and nothing uses it."},
	{Name: "else", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "if CONDITION { VALUE } else { VALUE }", Doc: "The value of a conditional expression when its condition is false."},
	{Name: "export", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "export NAME := VALUE", Doc: "An assignment that recipes also see as an environment variable."},
	{Name: "if", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "if CONDITION { VALUE } else { VALUE }", Doc: "A conditional expression."},
	{Name: "import", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "import? 'PATH'", Doc: "Include another justfile's items in this one; `import?` ignores a missing file."},
	{Name: "mod", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "mod? NAME 'PATH'", Doc: "A submodule whose recipes run as `NAME::recipe`; `mod?` ignores a missing file."},
	{Name: "set", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "set NAME := VALUE", Doc: "Change a setting."},
	{Name: "unexport", Kind: model.KindKeyword, Arity: model.NoArity, Signature: "unexport NAME", Doc: "Remove an environment variable before recipes run."},
}

var literalArguments = func() map[string][]string {
	out := map[string][]string{}
	for _, a := range attributeSpecs {
		if len(a.literals) > 0 {
			out[a.name] = a.literals
		}
	}
	return out
}()

func args(lowest, highest int) model.Arity { return model.Arity{Min: lowest, Max: highest} }

var Builtins = func() model.Builtins {
	var entries []model.Builtin
	for _, s := range settingSpecs {
		entries = append(entries, model.Builtin{Name: s.name, Kind: model.KindSetting, Signature: settingSignature(s), Doc: s.doc, Arity: model.NoArity, Deprecated: s.deprecated})
	}
	for _, a := range attributeSpecs {
		entries = append(entries, model.Builtin{Name: a.name, Kind: model.KindAttribute, Signature: "[" + a.name + "]", Doc: a.doc, Arity: a.arity})
	}
	for _, f := range functionSpecs {
		entries = append(entries, model.Builtin{Name: f.name, Kind: model.KindFunction, Signature: f.name + "(" + f.params + ")", Doc: f.doc, Arity: f.arity, Deprecated: f.deprecated})
	}
	for _, c := range constantSpecs {
		entries = append(entries, model.Builtin{Name: c.name, Kind: model.KindVariable, Signature: c.name + " := " + c.value, Doc: "A constant every justfile can use.", Arity: model.NoArity})
	}
	return model.NewBuiltins(entries, keywordDocs)
}()

var settingTypes = func() map[string]settingType {
	out := make(map[string]settingType, len(settingSpecs))
	for _, s := range settingSpecs {
		out[s.name] = s.kind
	}
	return out
}()

func settingSignature(s settingSpec) string {
	switch s.kind {
	case settingString:
		return "set " + s.name + " := 'VALUE'"
	case settingList:
		return "set " + s.name + " := ['COMMAND', 'ARG', ...]"
	case settingBool:
	}
	return "set " + s.name
}
