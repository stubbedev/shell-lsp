package builtins

const tableNixVersion = "2.34.9"

type gate struct {
	feature string
	note    string
}

func experimentalFeature(name string) gate {
	return gate{feature: name, note: "Needs the `" + name + "` experimental feature."}
}

var (
	gateFlakes             = experimentalFeature("flakes")
	gateFetchClosure       = experimentalFeature("fetch-closure")
	gateDynamicDerivations = experimentalFeature("dynamic-derivations")
	gateFetchTree          = gate{feature: "fetch-tree", note: "Needs the `fetch-tree` experimental feature, which `flakes` also turns on."}
	gateImpure             = gate{note: "Missing in pure evaluation mode."}
	gateUnsafeNative       = gate{note: "Only defined when `allow-unsafe-native-code-during-evaluation` is on."}
)

type member struct {
	name   string
	params []string
	doc    string
	global bool
	gate   gate
}

func (m member) fullDoc() string {
	if m.gate.note == "" {
		return m.doc
	}
	return m.doc + " " + m.gate.note
}

var parserGlobal = Builtin{
	Name:   "__curPos",
	Doc:    "The `{ file, line, column }` of this very expression, or `null` when it has no source file.",
	Global: true,
}

var members = []member{
	{name: "abort", params: []string{"s"}, global: true, doc: "Stops evaluation with the message `s`; unlike `throw`, `tryEval` cannot intercept it."},
	{name: "add", params: []string{"e1", "e2"}, doc: "Adds two numbers."},
	{name: "addDrvOutputDependencies", params: []string{"s"}, doc: "Upgrades a string's single plain context element on a `.drv` file to a dependency on all of that derivation's outputs."},
	{name: "addErrorContext", params: []string{"message", "value"}, doc: "Returns `value`, adding `message` to the trace of any error raised while forcing it."},
	{name: "all", params: []string{"pred", "list"}, doc: "Tells whether `pred` holds for every element of `list`; true for an empty list."},
	{name: "any", params: []string{"pred", "list"}, doc: "Tells whether `pred` holds for at least one element of `list`; false for an empty list."},
	{name: "appendContext", params: []string{"s", "context"}, doc: "Returns `s` with extra string context, given in the attribute-set shape that `getContext` produces."},
	{name: "attrNames", params: []string{"set"}, doc: "Lists the attribute names of `set` in sorted order."},
	{name: "attrValues", params: []string{"set"}, doc: "Lists the attribute values of `set`, ordered by their attribute names."},
	{name: "baseNameOf", params: []string{"x"}, global: true, doc: "Gives the last component of a path or string, ignoring a trailing slash."},
	{name: "bitAnd", params: []string{"e1", "e2"}, doc: "Bitwise AND of two integers."},
	{name: "bitOr", params: []string{"e1", "e2"}, doc: "Bitwise OR of two integers."},
	{name: "bitXor", params: []string{"e1", "e2"}, doc: "Bitwise exclusive OR of two integers."},
	{name: "break", params: []string{"v"}, global: true, doc: "Returns `v`, first pausing in the interactive debugger when evaluation runs with `--debugger`."},
	{name: "builtins", global: true, doc: "The attribute set holding every built-in function and constant."},
	{name: "catAttrs", params: []string{"attr", "list"}, doc: "Collects the value of attribute `attr` from each set in `list`, skipping sets without it."},
	{name: "ceil", params: []string{"number"}, doc: "Rounds a number up to the nearest integer."},
	{name: "compareVersions", params: []string{"s1", "s2"}, doc: "Compares two version strings component by component, returning `-1`, `0` or `1`."},
	{name: "concatLists", params: []string{"lists"}, doc: "Flattens a list of lists by one level."},
	{name: "concatMap", params: []string{"f", "list"}, doc: "Applies `f`, which returns a list, to each element and joins the results."},
	{name: "concatStringsSep", params: []string{"separator", "list"}, doc: "Joins a list of strings with `separator` between them."},
	{name: "convertHash", params: []string{"args"}, doc: "Rewrites a hash into another encoding, taking `hash`, an optional `hashAlgo` and `toHashFormat`."},
	{name: "currentSystem", doc: "The platform of the evaluating machine as a string such as `x86_64-linux`.", gate: gateImpure},
	{name: "currentTime", doc: "The Unix time in seconds at which evaluation started.", gate: gateImpure},
	{name: "deepSeq", params: []string{"e1", "e2"}, doc: "Forces `e1` completely, including everything nested inside it, then returns `e2`."},
	{name: "derivation", params: []string{"attrs"}, global: true, doc: "Describes a build from `name`, `system`, `builder` and further attributes, giving a set with `drvPath`, `outPath` and one attribute per output."},
	{name: "derivationStrict", params: []string{"attrs"}, global: true, doc: "The primitive beneath `derivation`, returning only the `.drv` path and the output paths."},
	{name: "dirOf", params: []string{"s"}, global: true, doc: "Gives the directory part of a path or string, or `.` when it has none."},
	{name: "div", params: []string{"e1", "e2"}, doc: "Divides two numbers, truncating when both are integers; the function behind the `/` operator."},
	{name: "elem", params: []string{"x", "xs"}, doc: "Tells whether `x` occurs in the list `xs`."},
	{name: "elemAt", params: []string{"xs", "n"}, doc: "Gives the element of the list `xs` at the zero-based index `n`."},
	{name: "exec", params: []string{"command"}, doc: "Runs the program named by an argument list during evaluation and evaluates its standard output as a Nix expression.", gate: gateUnsafeNative},
	{name: "false", global: true, doc: "The Boolean value false."},
	{name: "fetchClosure", params: []string{"args"}, doc: "Copies a store path and its dependencies from a binary cache, given `fromStore`, `fromPath` and optionally `toPath`.", gate: gateFetchClosure},
	{name: "fetchGit", params: []string{"args"}, global: true, doc: "Copies a Git repository revision into the store at evaluation time, from a URL string or a set with `url`, `ref`, `rev` and more."},
	{name: "fetchMercurial", params: []string{"args"}, global: true, doc: "Copies a Mercurial repository revision into the store at evaluation time; pure evaluation requires a revision."},
	{name: "fetchTarball", params: []string{"args"}, global: true, doc: "Downloads and unpacks an archive into the store at evaluation time, from a URL string or a set with `url` and `sha256`."},
	{name: "fetchTree", params: []string{"input"}, global: true, doc: "Fetches a source tree described by a typed attribute set or URL-like reference, returning its store path and metadata.", gate: gateFetchTree},
	{name: "fetchurl", params: []string{"arg"}, doc: "Downloads a single file into the store at evaluation time."},
	{name: "filter", params: []string{"f", "list"}, doc: "Keeps the elements of `list` for which `f` returns true."},
	{name: "filterSource", params: []string{"e1", "e2"}, doc: "Copies the path `e2` into the store, keeping only entries for which the predicate `e1`, called with a path and a file type, returns true."},
	{name: "findFile", params: []string{"search-path", "lookup-path"}, doc: "Resolves a `<...>` style lookup against a list of `{ prefix, path }` sets; the function that angle-bracket paths desugar to."},
	{name: "flakeRefToString", params: []string{"attrs"}, doc: "Renders a flake reference given as an attribute set in its URL-like string form.", gate: gateFlakes},
	{name: "floor", params: []string{"number"}, doc: "Rounds a number down to the nearest integer."},
	{name: "foldl'", params: []string{"op", "nul", "list"}, doc: "Folds `list` from the left, starting at `nul` and forcing the accumulator at each step."},
	{name: "fromJSON", params: []string{"e"}, doc: "Parses a JSON string into a Nix value."},
	{name: "fromTOML", params: []string{"e"}, global: true, doc: "Parses a TOML string into a Nix value."},
	{name: "functionArgs", params: []string{"f"}, doc: "Maps each named parameter of a set-pattern function to whether it has a default; empty for other functions."},
	{name: "genList", params: []string{"generator", "length"}, doc: "Builds a list of `length` elements by calling `generator` with each index from zero."},
	{name: "genericClosure", params: []string{"attrset"}, doc: "Computes a transitive closure from `startSet` using `operator`, deduplicating on each element's `key` attribute."},
	{name: "getAttr", params: []string{"s", "set"}, doc: "Selects the attribute of `set` named by the string `s`, failing when it is missing."},
	{name: "getContext", params: []string{"s"}, doc: "Exposes the string context of `s` as an attribute set keyed by store path."},
	{name: "getEnv", params: []string{"s"}, doc: "Reads an environment variable of the evaluator, giving an empty string when unset and always in pure evaluation."},
	{name: "getFlake", params: []string{"args"}, doc: "Fetches a flake from its reference and returns its evaluated outputs.", gate: gateFlakes},
	{name: "groupBy", params: []string{"f", "list"}, doc: "Groups the elements of `list` into a set of lists keyed by the string that `f` returns for each."},
	{name: "hasAttr", params: []string{"s", "set"}, doc: "Tells whether `set` has an attribute named by the string `s`."},
	{name: "hasContext", params: []string{"s"}, doc: "Tells whether the string `s` carries any string context."},
	{name: "hashFile", params: []string{"type", "p"}, doc: "Hashes the contents of the file `p` with the algorithm named by `type`, giving base-16 text."},
	{name: "hashString", params: []string{"type", "s"}, doc: "Hashes the string `s` with the algorithm named by `type`, giving base-16 text."},
	{name: "head", params: []string{"list"}, doc: "Gives the first element of a list, failing on an empty one."},
	{name: "import", params: []string{"path"}, global: true, doc: "Evaluates the Nix file at `path`, or its `default.nix` for a directory, and returns the value."},
	{name: "importNative", params: []string{"path", "symbol"}, doc: "Loads a shared library and calls the named symbol to produce a value.", gate: gateUnsafeNative},
	{name: "intersectAttrs", params: []string{"e1", "e2"}, doc: "Keeps the attributes of `e2` whose names also appear in `e1`."},
	{name: "isAttrs", params: []string{"e"}, doc: "Tells whether `e` is an attribute set."},
	{name: "isBool", params: []string{"e"}, doc: "Tells whether `e` is a Boolean."},
	{name: "isFloat", params: []string{"e"}, doc: "Tells whether `e` is a floating-point number."},
	{name: "isFunction", params: []string{"e"}, doc: "Tells whether `e` is a function."},
	{name: "isInt", params: []string{"e"}, doc: "Tells whether `e` is an integer."},
	{name: "isList", params: []string{"e"}, doc: "Tells whether `e` is a list."},
	{name: "isNull", params: []string{"e"}, global: true, doc: "Tells whether `e` is `null`, the same as comparing with `== null`."},
	{name: "isPath", params: []string{"e"}, doc: "Tells whether `e` is a path value."},
	{name: "isString", params: []string{"e"}, doc: "Tells whether `e` is a string."},
	{name: "langVersion", doc: "An integer identifying the version of the Nix language."},
	{name: "length", params: []string{"e"}, doc: "Gives the number of elements in a list."},
	{name: "lessThan", params: []string{"e1", "e2"}, doc: "Compares two numbers, strings, paths or lists, the function behind the `<` operator."},
	{name: "listToAttrs", params: []string{"e"}, doc: "Builds an attribute set from a list of `{ name, value }` sets; the first occurrence of a name wins."},
	{name: "map", params: []string{"f", "list"}, global: true, doc: "Applies `f` to each element of `list`, lazily."},
	{name: "mapAttrs", params: []string{"f", "attrset"}, doc: "Replaces each attribute value by `f name value`, keeping the names."},
	{name: "match", params: []string{"regex", "str"}, doc: "Matches an extended regular expression against the whole of `str`, giving the list of capture groups or `null`."},
	{name: "mul", params: []string{"e1", "e2"}, doc: "Multiplies two numbers, the function behind the `*` operator."},
	{name: "nixPath", doc: "The search path used by `<...>` lookups as a list of `{ prefix, path }` sets; empty in pure evaluation."},
	{name: "nixVersion", doc: "The version of the running Nix as a string."},
	{name: "null", global: true, doc: "The single value of the null type."},
	{name: "outputOf", params: []string{"derivation-reference", "output-name"}, doc: "Refers to a named output of a derivation that may itself be the output of another derivation.", gate: gateDynamicDerivations},
	{name: "parseDrvName", params: []string{"s"}, doc: "Splits a package name such as `foo-1.2` into a set with `name` and `version`."},
	{name: "parseFlakeRef", params: []string{"flake-ref"}, doc: "Parses a flake reference string into its attribute-set form.", gate: gateFlakes},
	{name: "partition", params: []string{"pred", "list"}, doc: "Splits `list` into a set with `right` for elements where `pred` holds and `wrong` for the rest."},
	{name: "path", params: []string{"args"}, doc: "Copies a filesystem path into the store, with optional `name`, `filter`, `recursive` and `sha256` settings."},
	{name: "pathExists", params: []string{"path"}, doc: "Tells whether a file or directory exists at `path`."},
	{name: "placeholder", params: []string{"output"}, global: true, doc: "Gives a stand-in string that is replaced by the real path of the named output when the derivation is built."},
	{name: "readDir", params: []string{"path"}, doc: "Lists a directory as a set mapping each entry name to its file type."},
	{name: "readFile", params: []string{"path"}, doc: "Gives the contents of a file as a string."},
	{name: "readFileType", params: []string{"p"}, doc: "Gives the file type at `p` as one of `regular`, `directory`, `symlink` or `unknown`, without following a final symlink."},
	{name: "removeAttrs", params: []string{"set", "list"}, global: true, doc: "Drops the attributes named in `list` from `set`; names it does not have are ignored."},
	{name: "replaceStrings", params: []string{"from", "to", "s"}, doc: "Substitutes in `s` each string of the list `from` by the matching string of the list `to`."},
	{name: "scopedImport", params: []string{"scope", "path"}, global: true, doc: "Like `import`, but with the attributes of `scope` also visible as variables in the imported file."},
	{name: "seq", params: []string{"e1", "e2"}, doc: "Forces `e1` to its outermost constructor only, then returns `e2`."},
	{name: "sort", params: []string{"comparator", "list"}, doc: "Sorts `list` stably using a strict less-than function."},
	{name: "split", params: []string{"regex", "str"}, doc: "Cuts `str` at each match of an extended regular expression, alternating the strings between matches with lists of capture groups."},
	{name: "splitVersion", params: []string{"s"}, doc: "Breaks a version string into the components that `compareVersions` works on."},
	{name: "storeDir", doc: "The directory of the Nix store as a string, usually `/nix/store`."},
	{name: "storePath", params: []string{"path"}, doc: "Turns a string naming an existing store path into one that depends on it, without copying; refused in pure evaluation."},
	{name: "stringLength", params: []string{"e"}, doc: "Gives the length of a string in bytes."},
	{name: "sub", params: []string{"e1", "e2"}, doc: "Subtracts `e2` from `e1`, the function behind the binary `-` operator."},
	{name: "substring", params: []string{"start", "len", "s"}, doc: "Takes `len` bytes of `s` from the byte offset `start`, stopping at the end of `s`; a negative `len` reads to the end."},
	{name: "tail", params: []string{"list"}, doc: "Gives a list without its first element, failing on an empty one."},
	{name: "throw", params: []string{"s"}, global: true, doc: "Raises an error with the message `s` that `tryEval` is able to catch."},
	{name: "toFile", params: []string{"name", "s"}, doc: "Writes the string `s` into a store file called `name` and returns its path."},
	{name: "toJSON", params: []string{"e"}, doc: "Serializes a value as a JSON string."},
	{name: "toPath", params: []string{"s"}, doc: "Checks that a string is an absolute path and returns it normalized as a string; a path literal or `/. + s` is the usual replacement."},
	{name: "toString", params: []string{"e"}, global: true, doc: "Coerces a value to a string, accepting more types than interpolation does, such as numbers, Booleans, `null` and lists."},
	{name: "toXML", params: []string{"e"}, doc: "Serializes a value as an XML string."},
	{name: "trace", params: []string{"e1", "e2"}, doc: "Prints `e1` to standard error when forced, then returns `e2`."},
	{name: "traceVerbose", params: []string{"e1", "e2"}, doc: "Behaves like `trace` under `--trace-verbose`, and otherwise just returns `e2`."},
	{name: "true", global: true, doc: "The Boolean value true."},
	{name: "tryEval", params: []string{"e"}, doc: "Forces `e` shallowly and gives `{ success, value }`, turning catchable errors such as `throw` into `success = false`."},
	{name: "typeOf", params: []string{"e"}, doc: "Names the type of a value, such as `int`, `string`, `set` or `lambda`."},
	{name: "unsafeDiscardOutputDependency", params: []string{"s"}, doc: "Downgrades a string's dependency on all outputs of a `.drv` file to a plain dependency on the file itself."},
	{name: "unsafeDiscardStringContext", params: []string{"s"}, doc: "Returns `s` with all of its string context removed, so its dependencies are no longer tracked."},
	{name: "unsafeGetAttrPos", params: []string{"s", "set"}, doc: "Gives the `{ file, line, column }` where the attribute `s` of `set` was defined, or `null` when unknown."},
	{name: "warn", params: []string{"e1", "e2"}, doc: "Reports the string `e1` as a warning when forced, then returns `e2`."},
	{name: "zipAttrsWith", params: []string{"f", "list"}, doc: "Merges a list of sets by calling `f name values` once per attribute name with all of that name's values."},
}
