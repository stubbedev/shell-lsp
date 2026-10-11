package awk

import "github.com/stubbedev/shell-lsp/internal/embedded/lang"

var functions = lang.Docs{
	"length":   {"length([s])", "Number of characters in `s`, or in `$0` when called without an argument. Given an array, gawk returns its number of elements."},
	"substr":   {"substr(s, m [, n])", "The substring of `s` starting at character `m` (1-based) with at most `n` characters, or up to the end of `s` when `n` is omitted."},
	"index":    {"index(s, t)", "Position (1-based) of the first occurrence of `t` in `s`, or 0 when `t` does not occur."},
	"split":    {"split(s, a [, fs])", "Splits `s` into the elements `a[1]` … `a[n]` on the field separator `fs` (default `FS`) and returns `n`. Earlier elements of `a` are deleted."},
	"sub":      {"sub(ere, repl [, in])", "Replaces the first match of `ere` in `in` (default `$0`) with `repl`, where `&` stands for the matched text. Returns the number of substitutions (0 or 1)."},
	"gsub":     {"gsub(ere, repl [, in])", "Like `sub`, but replaces every non-overlapping match. Returns the number of substitutions."},
	"match":    {"match(s, ere [, arr])", "Position of the leftmost longest match of `ere` in `s`, or 0. Sets `RSTART` and `RLENGTH`; gawk also fills `arr` with the matched groups."},
	"sprintf":  {"sprintf(fmt, expr, ...)", "Formats the expressions like `printf` and returns the resulting string instead of printing it."},
	"sin":      {"sin(x)", "Sine of `x` in radians."},
	"cos":      {"cos(x)", "Cosine of `x` in radians."},
	"atan2":    {"atan2(y, x)", "Arctangent of `y/x` in radians, in the range -π to π."},
	"exp":      {"exp(x)", "The exponential function e^`x`."},
	"log":      {"log(x)", "Natural logarithm of `x`."},
	"sqrt":     {"sqrt(x)", "Square root of `x`."},
	"int":      {"int(x)", "`x` truncated toward zero to an integer."},
	"rand":     {"rand()", "A random number `n` with 0 ≤ `n` < 1."},
	"srand":    {"srand([expr])", "Seeds `rand` with `expr`, or with the time of day when omitted. Returns the previous seed."},
	"tolower":  {"tolower(s)", "`s` with every uppercase letter converted to lowercase."},
	"toupper":  {"toupper(s)", "`s` with every lowercase letter converted to uppercase."},
	"system":   {"system(cmd)", "Runs `cmd` with the shell and returns its exit status."},
	"close":    {"close(expr)", "Closes the file or pipe opened by `print`, `printf` or `getline` with the same `expr`. Returns 0 on success."},
	"fflush":   {"fflush([expr])", "Flushes the output file or pipe named by `expr`, or every open output when called without an argument."},
	"gensub":   {"gensub(ere, repl, how [, target])", "gawk: returns `target` (default `$0`) with matches of `ere` replaced; `how` is `\"g\"` for all matches or a number for the nth. `\\\\N` in `repl` refers to group N."},
	"strftime": {"strftime([format [, timestamp [, utc]]])", "gawk: formats `timestamp` (default now) with the strftime(3) `format`."},
	"systime":  {"systime()", "gawk: the current time as seconds since the epoch."},
	"mktime":   {"mktime(\"YYYY MM DD HH MM SS [DST]\" [, utc])", "gawk: converts a date specification to seconds since the epoch."},
	"asort":    {"asort(source [, dest [, how]])", "gawk: sorts the values of `source` and renumbers them 1 … n (into `dest` when given). Returns n."},
	"asorti":   {"asorti(source [, dest [, how]])", "gawk: sorts the indices of `source` into the values of `dest` (or `source`), numbered 1 … n. Returns n."},
	"patsplit": {"patsplit(s, a [, fieldpat [, seps]])", "gawk: splits `s` into `a` with the fields being matches of `fieldpat` (default `FPAT`). Returns the number of fields."},
	"and":      {"and(v1, v2, ...)", "gawk: bitwise AND of the arguments."},
	"or":       {"or(v1, v2, ...)", "gawk: bitwise OR of the arguments."},
	"xor":      {"xor(v1, v2, ...)", "gawk: bitwise XOR of the arguments."},
	"compl":    {"compl(v)", "gawk: bitwise complement of `v`."},
	"lshift":   {"lshift(v, n)", "gawk: `v` shifted left by `n` bits."},
	"rshift":   {"rshift(v, n)", "gawk: `v` shifted right by `n` bits."},
	"strtonum": {"strtonum(s)", "gawk: the numeric value of `s`, honouring `0x` hexadecimal and leading-zero octal prefixes."},
	"isarray":  {"isarray(x)", "gawk: 1 when `x` is an array, otherwise 0."},
	"typeof":   {"typeof(x)", "gawk: the type of `x` as a string: `\"array\"`, `\"number\"`, `\"string\"`, `\"strnum\"`, `\"regexp\"`, `\"unassigned\"` or `\"untyped\"`."},
}

var variables = lang.Docs{
	"NR":          {"NR", "Number of input records read so far, across all input files."},
	"NF":          {"NF", "Number of fields in the current record. Assigning to it rebuilds `$0`."},
	"FNR":         {"FNR", "Number of the current record within the current input file."},
	"FS":          {"FS", "Input field separator: a single space (the default) splits on runs of blanks, another single character splits on it, anything longer is an extended regular expression."},
	"OFS":         {"OFS", "Output field separator printed between the arguments of `print`; default a single space."},
	"ORS":         {"ORS", "Output record separator printed after each `print`; default a newline."},
	"RS":          {"RS", "Input record separator; default a newline. An empty string separates records by blank lines."},
	"FILENAME":    {"FILENAME", "Name of the current input file; undefined inside `BEGIN`."},
	"SUBSEP":      {"SUBSEP", "Separator joining the subscripts of a multi-dimensional array index `a[i, j]`; default `\"\\034\"`."},
	"RSTART":      {"RSTART", "Start position of the string matched by the last `match` call, or 0."},
	"RLENGTH":     {"RLENGTH", "Length of the string matched by the last `match` call, or -1."},
	"CONVFMT":     {"CONVFMT", "printf format for converting numbers to strings; default `\"%.6g\"`."},
	"OFMT":        {"OFMT", "printf format `print` uses for numbers; default `\"%.6g\"`."},
	"ENVIRON":     {"ENVIRON", "Array of the environment variables, indexed by name."},
	"ARGC":        {"ARGC", "Number of elements in `ARGV`."},
	"ARGV":        {"ARGV", "Array of the command-line arguments, `ARGV[0]` being the program name; options and the program text are excluded."},
	"IGNORECASE":  {"IGNORECASE", "gawk: when non-zero, regular expression matches and string comparisons ignore case."},
	"RT":          {"RT", "gawk: the text that matched `RS` for the current record."},
	"FPAT":        {"FPAT", "gawk: regular expression describing the contents of a field, instead of the separator between fields."},
	"FIELDWIDTHS": {"FIELDWIDTHS", "gawk: space-separated list of fixed field widths used to split records."},
	"PROCINFO":    {"PROCINFO", "gawk: array of information about the running program, such as `PROCINFO[\"pid\"]` and `PROCINFO[\"sorted_in\"]`."},
	"BINMODE":     {"BINMODE", "gawk: binary mode for input and output on non-POSIX systems."},
	"LINT":        {"LINT", "gawk: enables lint warnings at runtime when non-zero."},
	"TEXTDOMAIN":  {"TEXTDOMAIN", "gawk: text domain used to translate `_\"strings\"`."},
	"ERRNO":       {"ERRNO", "gawk: description of the last system error from `getline`, `close` or a redirection."},
	"ARGIND":      {"ARGIND", "gawk: index in `ARGV` of the file being read."},
	"SYMTAB":      {"SYMTAB", "gawk: array of every global variable, indexed by name."},
	"FUNCTAB":     {"FUNCTAB", "gawk: array of every function name."},
}

func Doc(name string, kind lang.Kind) (lang.Doc, bool) {
	table := variables
	if kind == lang.Function {
		table = functions
	}
	return table.Lookup(name)
}
