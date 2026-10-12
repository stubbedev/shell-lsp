package awk

import "github.com/stubbedev/shell-lsp/internal/embedded/lang"

var functions = lang.Docs{
	"length":   {Signature: "length([s])", Summary: "Number of characters in `s`, or in `$0` when called without an argument. Given an array, gawk returns its number of elements."},
	"substr":   {Signature: "substr(s, m [, n])", Summary: "The substring of `s` starting at character `m` (1-based) with at most `n` characters, or up to the end of `s` when `n` is omitted."},
	"index":    {Signature: "index(s, t)", Summary: "Position (1-based) of the first occurrence of `t` in `s`, or 0 when `t` does not occur."},
	"split":    {Signature: "split(s, a [, fs])", Summary: "Splits `s` into the elements `a[1]` … `a[n]` on the field separator `fs` (default `FS`) and returns `n`. Earlier elements of `a` are deleted."},
	"sub":      {Signature: "sub(ere, repl [, in])", Summary: "Replaces the first match of `ere` in `in` (default `$0`) with `repl`, where `&` stands for the matched text. Returns the number of substitutions (0 or 1)."},
	"gsub":     {Signature: "gsub(ere, repl [, in])", Summary: "Like `sub`, but replaces every non-overlapping match. Returns the number of substitutions."},
	"match":    {Signature: "match(s, ere [, arr])", Summary: "Position of the leftmost longest match of `ere` in `s`, or 0. Sets `RSTART` and `RLENGTH`; gawk also fills `arr` with the matched groups."},
	"sprintf":  {Signature: "sprintf(fmt, expr, ...)", Summary: "Formats the expressions like `printf` and returns the resulting string instead of printing it."},
	"sin":      {Signature: "sin(x)", Summary: "Sine of `x` in radians."},
	"cos":      {Signature: "cos(x)", Summary: "Cosine of `x` in radians."},
	"atan2":    {Signature: "atan2(y, x)", Summary: "Arctangent of `y/x` in radians, in the range -π to π."},
	"exp":      {Signature: "exp(x)", Summary: "The exponential function e^`x`."},
	"log":      {Signature: "log(x)", Summary: "Natural logarithm of `x`."},
	"sqrt":     {Signature: "sqrt(x)", Summary: "Square root of `x`."},
	"int":      {Signature: "int(x)", Summary: "`x` truncated toward zero to an integer."},
	"rand":     {Signature: "rand()", Summary: "A random number `n` with 0 ≤ `n` < 1."},
	"srand":    {Signature: "srand([expr])", Summary: "Seeds `rand` with `expr`, or with the time of day when omitted. Returns the previous seed."},
	"tolower":  {Signature: "tolower(s)", Summary: "`s` with every uppercase letter converted to lowercase."},
	"toupper":  {Signature: "toupper(s)", Summary: "`s` with every lowercase letter converted to uppercase."},
	"system":   {Signature: "system(cmd)", Summary: "Runs `cmd` with the shell and returns its exit status."},
	"close":    {Signature: "close(expr)", Summary: "Closes the file or pipe opened by `print`, `printf` or `getline` with the same `expr`. Returns 0 on success."},
	"fflush":   {Signature: "fflush([expr])", Summary: "Flushes the output file or pipe named by `expr`, or every open output when called without an argument."},
	"gensub":   {Signature: "gensub(ere, repl, how [, target])", Summary: "gawk: returns `target` (default `$0`) with matches of `ere` replaced; `how` is `\"g\"` for all matches or a number for the nth. `\\\\N` in `repl` refers to group N."},
	"strftime": {Signature: "strftime([format [, timestamp [, utc]]])", Summary: "gawk: formats `timestamp` (default now) with the strftime(3) `format`."},
	"systime":  {Signature: "systime()", Summary: "gawk: the current time as seconds since the epoch."},
	"mktime":   {Signature: "mktime(\"YYYY MM DD HH MM SS [DST]\" [, utc])", Summary: "gawk: converts a date specification to seconds since the epoch."},
	"asort":    {Signature: "asort(source [, dest [, how]])", Summary: "gawk: sorts the values of `source` and renumbers them 1 … n (into `dest` when given). Returns n."},
	"asorti":   {Signature: "asorti(source [, dest [, how]])", Summary: "gawk: sorts the indices of `source` into the values of `dest` (or `source`), numbered 1 … n. Returns n."},
	"patsplit": {Signature: "patsplit(s, a [, fieldpat [, seps]])", Summary: "gawk: splits `s` into `a` with the fields being matches of `fieldpat` (default `FPAT`). Returns the number of fields."},
	"and":      {Signature: "and(v1, v2, ...)", Summary: "gawk: bitwise AND of the arguments."},
	"or":       {Signature: "or(v1, v2, ...)", Summary: "gawk: bitwise OR of the arguments."},
	"xor":      {Signature: "xor(v1, v2, ...)", Summary: "gawk: bitwise XOR of the arguments."},
	"compl":    {Signature: "compl(v)", Summary: "gawk: bitwise complement of `v`."},
	"lshift":   {Signature: "lshift(v, n)", Summary: "gawk: `v` shifted left by `n` bits."},
	"rshift":   {Signature: "rshift(v, n)", Summary: "gawk: `v` shifted right by `n` bits."},
	"strtonum": {Signature: "strtonum(s)", Summary: "gawk: the numeric value of `s`, honouring `0x` hexadecimal and leading-zero octal prefixes."},
	"isarray":  {Signature: "isarray(x)", Summary: "gawk: 1 when `x` is an array, otherwise 0."},
	"typeof":   {Signature: "typeof(x)", Summary: "gawk: the type of `x` as a string: `\"array\"`, `\"number\"`, `\"string\"`, `\"strnum\"`, `\"regexp\"`, `\"unassigned\"` or `\"untyped\"`."},
}

var variables = lang.Docs{
	"NR":          {Signature: "NR", Summary: "Number of input records read so far, across all input files."},
	"NF":          {Signature: "NF", Summary: "Number of fields in the current record. Assigning to it rebuilds `$0`."},
	"FNR":         {Signature: "FNR", Summary: "Number of the current record within the current input file."},
	"FS":          {Signature: "FS", Summary: "Input field separator: a single space (the default) splits on runs of blanks, another single character splits on it, anything longer is an extended regular expression."},
	"OFS":         {Signature: "OFS", Summary: "Output field separator printed between the arguments of `print`; default a single space."},
	"ORS":         {Signature: "ORS", Summary: "Output record separator printed after each `print`; default a newline."},
	"RS":          {Signature: "RS", Summary: "Input record separator; default a newline. An empty string separates records by blank lines."},
	"FILENAME":    {Signature: "FILENAME", Summary: "Name of the current input file; undefined inside `BEGIN`."},
	"SUBSEP":      {Signature: "SUBSEP", Summary: "Separator joining the subscripts of a multi-dimensional array index `a[i, j]`; default `\"\\034\"`."},
	"RSTART":      {Signature: "RSTART", Summary: "Start position of the string matched by the last `match` call, or 0."},
	"RLENGTH":     {Signature: "RLENGTH", Summary: "Length of the string matched by the last `match` call, or -1."},
	"CONVFMT":     {Signature: "CONVFMT", Summary: "printf format for converting numbers to strings; default `\"%.6g\"`."},
	"OFMT":        {Signature: "OFMT", Summary: "printf format `print` uses for numbers; default `\"%.6g\"`."},
	"ENVIRON":     {Signature: "ENVIRON", Summary: "Array of the environment variables, indexed by name."},
	"ARGC":        {Signature: "ARGC", Summary: "Number of elements in `ARGV`."},
	"ARGV":        {Signature: "ARGV", Summary: "Array of the command-line arguments, `ARGV[0]` being the program name; options and the program text are excluded."},
	"IGNORECASE":  {Signature: "IGNORECASE", Summary: "gawk: when non-zero, regular expression matches and string comparisons ignore case."},
	"RT":          {Signature: "RT", Summary: "gawk: the text that matched `RS` for the current record."},
	"FPAT":        {Signature: "FPAT", Summary: "gawk: regular expression describing the contents of a field, instead of the separator between fields."},
	"FIELDWIDTHS": {Signature: "FIELDWIDTHS", Summary: "gawk: space-separated list of fixed field widths used to split records."},
	"PROCINFO":    {Signature: "PROCINFO", Summary: "gawk: array of information about the running program, such as `PROCINFO[\"pid\"]` and `PROCINFO[\"sorted_in\"]`."},
	"BINMODE":     {Signature: "BINMODE", Summary: "gawk: binary mode for input and output on non-POSIX systems."},
	"LINT":        {Signature: "LINT", Summary: "gawk: enables lint warnings at runtime when non-zero."},
	"TEXTDOMAIN":  {Signature: "TEXTDOMAIN", Summary: "gawk: text domain used to translate `_\"strings\"`."},
	"ERRNO":       {Signature: "ERRNO", Summary: "gawk: description of the last system error from `getline`, `close` or a redirection."},
	"ARGIND":      {Signature: "ARGIND", Summary: "gawk: index in `ARGV` of the file being read."},
	"SYMTAB":      {Signature: "SYMTAB", Summary: "gawk: array of every global variable, indexed by name."},
	"FUNCTAB":     {Signature: "FUNCTAB", Summary: "gawk: array of every function name."},
}

func Doc(name string, tok lang.Token) (lang.Doc, bool) {
	table := variables
	if tok.Kind == lang.Function {
		table = functions
	}
	doc, ok := table[name]
	return doc, ok
}
