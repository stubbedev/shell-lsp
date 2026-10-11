# Checks

Every check the linter runs, generated from the lint catalog by
`go test ./internal/lint -run TestChecksDoc -update`. `SC` codes reuse ShellCheck's numbering;
`SL` codes are shell-lsp's own and are described in [rules.md](rules.md). Optional checks run only
when switched on by name with `enable=`, `--enable` or the `lint.optional` setting. The Minimal column
marks the checks the `minimal` profile keeps; see [Profiles](../README.md#profiles).

| Code | Severity | Optional | Minimal | Summary |
| --- | --- | --- | --- | --- |
| [SC1001](https://www.shellcheck.net/wiki/SC1001) | info |  |  | A backslash before an ordinary character outside quotes does nothing. |
| [SC1003](https://www.shellcheck.net/wiki/SC1003) | info |  | yes | A backslash cannot escape a single quote inside single quotes, so the quote ends the string. |
| [SC1007](https://www.shellcheck.net/wiki/SC1007) | warning |  | yes | A space after = ends the assignment, so the variable is set to empty for the next command only. |
| [SC1008](https://www.shellcheck.net/wiki/SC1008) | error |  | yes | The shebang names an interpreter that is not a shell this linter recognises. |
| [SC1010](https://www.shellcheck.net/wiki/SC1010) | warning |  | yes | A keyword is read as a plain word because no ; or line break comes before it. |
| [SC1011](https://www.shellcheck.net/wiki/SC1011) | warning |  | yes | An apostrophe inside single quotes closes the string, leaving the rest outside the quotes. |
| [SC1012](https://www.shellcheck.net/wiki/SC1012) | warning |  | yes | An escape like \\n or \\t outside quotes is just the letter, not a control character. |
| [SC1014](https://www.shellcheck.net/wiki/SC1014) | warning |  | yes | Brackets around a command test the command's name as a string instead of running it. |
| [SC1017](https://www.shellcheck.net/wiki/SC1017) | error |  | yes | The file contains a literal carriage return, usually from Windows line endings. |
| [SC1018](https://www.shellcheck.net/wiki/SC1018) | error |  | yes | A Unicode space is not a plain space, so the shell does not split words on it. |
| [SC1019](https://www.shellcheck.net/wiki/SC1019) | error |  | yes | A unary test operator has no operand. |
| [SC1020](https://www.shellcheck.net/wiki/SC1020) | error |  | yes | A \] glued to the previous word is part of that word and does not end the test. |
| [SC1021](https://www.shellcheck.net/wiki/SC1021) | error |  | yes | An escaped \\) glued to the previous word does not close the group. |
| [SC1026](https://www.shellcheck.net/wiki/SC1026) | error |  | yes | A nested \[ does not group conditions inside \[ \]. |
| [SC1027](https://www.shellcheck.net/wiki/SC1027) | error |  | yes | A comparison operator has no right-hand operand. |
| [SC1034](https://www.shellcheck.net/wiki/SC1034) | error |  | yes | A test opens with a single \[ but closes with \]\]. |
| [SC1035](https://www.shellcheck.net/wiki/SC1035) | error |  | yes | A test bracket needs a space between it and the next word. |
| [SC1037](https://www.shellcheck.net/wiki/SC1037) | error |  | yes | $ followed by several digits expands only the first digit as a positional parameter. |
| [SC1039](https://www.shellcheck.net/wiki/SC1039) | error |  | yes | An indented terminator does not end the here-document. |
| [SC1040](https://www.shellcheck.net/wiki/SC1040) | error |  | yes | A terminator indented with spaces does not end a \<\<- here-document, which strips only tabs. |
| [SC1064](https://www.shellcheck.net/wiki/SC1064) | error |  | yes | A function body is not a compound command. |
| [SC1071](https://www.shellcheck.net/wiki/SC1071) | error |  | yes | A file run by an interpreter that is not a shell is not linted. |
| [SC1075](https://www.shellcheck.net/wiki/SC1075) | error |  | yes | else if starts a nested if that needs its own fi; elif was probably meant. |
| [SC1076](https://www.shellcheck.net/wiki/SC1076) | error |  | yes | \[ \] does not evaluate arithmetic, so an operator there is part of a word. |
| [SC1077](https://www.shellcheck.net/wiki/SC1077) | error |  | yes | An acute accent is used where a backtick was meant. |
| [SC1078](https://www.shellcheck.net/wiki/SC1078) | warning |  | yes | A quote that spans several lines closes right before more text, which suggests it is unbalanced. |
| [SC1079](https://www.shellcheck.net/wiki/SC1079) | info |  | yes | A quote that looks like it opens a new string closes an earlier multi-line one. |
| [SC1080](https://www.shellcheck.net/wiki/SC1080) | error |  | yes | A \[ test continues on the next line without a backslash before the line break. |
| [SC1081](https://www.shellcheck.net/wiki/SC1081) | error |  | yes | Keywords are case sensitive, so a capitalised keyword runs as a command. |
| [SC1082](https://www.shellcheck.net/wiki/SC1082) | error |  | yes | A UTF-8 byte order mark at the start of the file makes the shell misread the first line. |
| [SC1083](https://www.shellcheck.net/wiki/SC1083) | warning |  | yes | A brace here is a literal character, not part of an expansion or a block. |
| [SC1084](https://www.shellcheck.net/wiki/SC1084) | error |  | yes | The shebang is written !# instead of #!. |
| [SC1087](https://www.shellcheck.net/wiki/SC1087) | error |  | yes | $name\[ expands the variable and appends a literal bracket instead of indexing the array. |
| [SC1090](https://www.shellcheck.net/wiki/SC1090) | warning |  | yes | A sourced file is only known at run time, so it cannot be followed. |
| [SC1091](https://www.shellcheck.net/wiki/SC1091) | info |  |  | A sourced file was not found, so it is not followed. |
| [SC1094](https://www.shellcheck.net/wiki/SC1094) | warning |  | yes | A sourced file has syntax errors, so what it defines may be missing. |
| [SC1097](https://www.shellcheck.net/wiki/SC1097) | error |  | yes | == in an assignment assigns a value starting with = instead of comparing. |
| [SC1099](https://www.shellcheck.net/wiki/SC1099) | error |  | yes | A # glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1100](https://www.shellcheck.net/wiki/SC1100) | error |  | yes | A Unicode dash is used where a hyphen-minus test operator was meant. |
| [SC1101](https://www.shellcheck.net/wiki/SC1101) | error |  | yes | A backslash escapes trailing whitespace instead of the line break, so the command ends there. |
| [SC1103](https://www.shellcheck.net/wiki/SC1103) | error |  | yes | A shell directive names a shell this linter does not know. |
| [SC1104](https://www.shellcheck.net/wiki/SC1104) | error |  | yes | A first line that looks like a shebang is missing its #. |
| [SC1107](https://www.shellcheck.net/wiki/SC1107) | warning |  | yes | A directive uses a key that does not exist. |
| [SC1108](https://www.shellcheck.net/wiki/SC1108) | error |  | yes | A test operator glued to its operand is part of the word, not a comparison. |
| [SC1109](https://www.shellcheck.net/wiki/SC1109) | error |  | yes | An HTML entity copied from a web page is used instead of the character itself. |
| [SC1110](https://www.shellcheck.net/wiki/SC1110) | warning |  | yes | A curly quote is a literal character, not shell quoting. |
| [SC1111](https://www.shellcheck.net/wiki/SC1111) | warning |  | yes | A curly double quote is a literal character inside double quotes or a here-document. |
| [SC1112](https://www.shellcheck.net/wiki/SC1112) | warning |  | yes | A curly single quote is a literal character inside single quotes. |
| [SC1113](https://www.shellcheck.net/wiki/SC1113) | error |  | yes | A first line that looks like a shebang is missing its !. |
| [SC1114](https://www.shellcheck.net/wiki/SC1114) | error |  | yes | Whitespace before #! keeps the shebang from working. |
| [SC1115](https://www.shellcheck.net/wiki/SC1115) | error |  | yes | Whitespace between # and ! keeps the shebang from working. |
| [SC1118](https://www.shellcheck.net/wiki/SC1118) | error |  | yes | A here-document terminator followed by whitespace does not end the here-document. |
| [SC1119](https://www.shellcheck.net/wiki/SC1119) | error |  | yes | A here-document terminator followed by ) does not end the here-document. |
| [SC1120](https://www.shellcheck.net/wiki/SC1120) | error |  | yes | A here-document terminator followed by a comment does not end the here-document. |
| [SC1121](https://www.shellcheck.net/wiki/SC1121) | error |  | yes | A here-document terminator followed by an operator does not end the here-document. |
| [SC1122](https://www.shellcheck.net/wiki/SC1122) | error |  | yes | A here-document terminator followed by more text does not end the here-document. |
| [SC1123](https://www.shellcheck.net/wiki/SC1123) | error |  | yes | A directive stands in front of a keyword where it has no command to apply to. |
| [SC1124](https://www.shellcheck.net/wiki/SC1124) | error |  | yes | A directive in front of a case branch pattern has no effect. |
| [SC1125](https://www.shellcheck.net/wiki/SC1125) | error |  | yes | A disable list contains something that is not a check code or range. |
| [SC1126](https://www.shellcheck.net/wiki/SC1126) | error |  | yes | A directive placed after a command's words or inside a continued list is rejected by ShellCheck. |
| [SC1127](https://www.shellcheck.net/wiki/SC1127) | error |  | yes | A comment starts with // or a similar marker instead of #, so the line runs as a command. |
| [SC1128](https://www.shellcheck.net/wiki/SC1128) | error |  | yes | A shebang that is not on the first line has no effect. |
| [SC1129](https://www.shellcheck.net/wiki/SC1129) | error |  | yes | A ! glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1130](https://www.shellcheck.net/wiki/SC1130) | error |  | yes | A : glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1131](https://www.shellcheck.net/wiki/SC1131) | error |  | yes | elsif, elseif or a similar spelling is not a keyword; the shell only has elif. |
| [SC1132](https://www.shellcheck.net/wiki/SC1132) | warning |  | yes | An unquoted & inside a word, such as in a URL, sends the command to the background. |
| [SC1135](https://www.shellcheck.net/wiki/SC1135) | style |  |  | Closing quotes right after $ only to keep it literal is confusing. |
| [SC1139](https://www.shellcheck.net/wiki/SC1139) | error |  | yes | -a or -o joins test commands outside \[ \], where it is just another word. |
| [SC1140](https://www.shellcheck.net/wiki/SC1140) | error |  | yes | Words after the closing \] become arguments of the test. |
| [SC1143](https://www.shellcheck.net/wiki/SC1143) | error |  | yes | A commented-out line ends a continued command, so the lines below run separately. |
| [SC1144](https://www.shellcheck.net/wiki/SC1144) | error |  | yes | external-sources=true only takes effect in a .shellcheckrc file. |
| [SC1145](https://www.shellcheck.net/wiki/SC1145) | error |  | yes | external-sources is set to something other than true or false. |
| [SC1146](https://www.shellcheck.net/wiki/SC1146) | error |  | yes | extended-analysis is set to something other than true or false. |
| [SC2000](https://www.shellcheck.net/wiki/SC2000) | style |  |  | echo piped to wc counts a string's length that ${#var} gives directly. |
| [SC2001](https://www.shellcheck.net/wiki/SC2001) | style |  |  | sed runs on a single value that a ${var//pattern/replacement} expansion could handle. |
| [SC2002](https://www.shellcheck.net/wiki/SC2002) | style | `useless-use-of-cat` |  | cat only feeds a file to the next command, which could read it directly. |
| [SC2003](https://www.shellcheck.net/wiki/SC2003) | style |  |  | expr is an antiquated way to do arithmetic. |
| [SC2004](https://www.shellcheck.net/wiki/SC2004) | style |  |  | The $ on a variable inside arithmetic is unnecessary. |
| [SC2005](https://www.shellcheck.net/wiki/SC2005) | style |  |  | echo only repeats the output of a command substitution. |
| [SC2006](https://www.shellcheck.net/wiki/SC2006) | style |  |  | Backtick command substitution is legacy syntax that nests and escapes poorly. |
| [SC2007](https://www.shellcheck.net/wiki/SC2007) | style |  |  | $\[ \] is an obsolete arithmetic form. |
| [SC2009](https://www.shellcheck.net/wiki/SC2009) | info |  | yes | Grepping ps output also matches the grep itself. |
| [SC2010](https://www.shellcheck.net/wiki/SC2010) | warning |  | yes | Filtering ls output with grep breaks on unusual file names. |
| [SC2011](https://www.shellcheck.net/wiki/SC2011) | warning |  | yes | Piping ls output to xargs breaks on file names with spaces. |
| [SC2012](https://www.shellcheck.net/wiki/SC2012) | info |  | yes | Parsing ls output breaks on unusual file names. |
| [SC2013](https://www.shellcheck.net/wiki/SC2013) | info |  | yes | A for loop over command output splits it into words, not lines. |
| [SC2014](https://www.shellcheck.net/wiki/SC2014) | info |  | yes | An expansion in a find -exec command runs once before find starts, not once per file. |
| [SC2015](https://www.shellcheck.net/wiki/SC2015) | info |  | yes | A && B \|\| C is not if-then-else: C also runs when A succeeds and B fails. |
| [SC2016](https://www.shellcheck.net/wiki/SC2016) | info |  |  | Single quotes keep $ and backticks literal, so nothing in them is expanded. |
| [SC2017](https://www.shellcheck.net/wiki/SC2017) | info |  | yes | Integer division truncates before a later multiplication, losing precision. |
| [SC2018](https://www.shellcheck.net/wiki/SC2018) | info |  | yes | The range a-z misses accented and other non-ASCII lowercase letters. |
| [SC2019](https://www.shellcheck.net/wiki/SC2019) | info |  | yes | The range A-Z misses accented and other non-ASCII uppercase letters. |
| [SC2020](https://www.shellcheck.net/wiki/SC2020) | info |  | yes | tr maps single characters, not words, so a repeated letter only uses its last mapping. |
| [SC2021](https://www.shellcheck.net/wiki/SC2021) | info |  | yes | Brackets around a tr range are matched as literal characters. |
| [SC2022](https://www.shellcheck.net/wiki/SC2022) | info |  | yes | In a regex, x\* repeats the character before it rather than matching anything after it. |
| [SC2023](https://www.shellcheck.net/wiki/SC2023) | info |  | yes | The time keyword only accepts -p, so other options need /usr/bin/time. |
| [SC2024](https://www.shellcheck.net/wiki/SC2024) | warning |  | yes | sudo or doas does not apply to a redirection, which the shell opens with the caller's permissions. |
| [SC2025](https://www.shellcheck.net/wiki/SC2025) | info |  | yes | Terminal escape sequences in PS1 are not wrapped in \\\[ \\\], so bash miscounts the prompt's width. |
| [SC2026](https://www.shellcheck.net/wiki/SC2026) | info |  | yes | A word between single-quoted strings is not quoted. |
| [SC2027](https://www.shellcheck.net/wiki/SC2027) | warning |  | yes | Quotes inside a string close and reopen it, leaving the inner part unquoted. |
| [SC2028](https://www.shellcheck.net/wiki/SC2028) | info |  |  | echo does not reliably interpret backslash escapes. |
| [SC2029](https://www.shellcheck.net/wiki/SC2029) | info |  | yes | An expansion in an ssh command runs on the client, not on the server. |
| [SC2030](https://www.shellcheck.net/wiki/SC2030) | info |  | yes | A variable changed in a subshell loses the change when the subshell exits. |
| [SC2031](https://www.shellcheck.net/wiki/SC2031) | info |  | yes | A variable read here still has its value from before a change made in a subshell. |
| [SC2032](https://www.shellcheck.net/wiki/SC2032) | info |  | yes | A command that runs external programs is given a function this script defines. |
| [SC2033](https://www.shellcheck.net/wiki/SC2033) | warning |  | yes | A command that runs external programs cannot see a shell function or alias. |
| [SC2034](https://www.shellcheck.net/wiki/SC2034) | warning |  | yes | A variable is assigned but never read. |
| [SC2035](https://www.shellcheck.net/wiki/SC2035) | info |  | yes | A file matched by a glob may start with a dash and be read as an option. |
| [SC2036](https://www.shellcheck.net/wiki/SC2036) | warning |  | yes | A pipeline stage that only assigns a variable pipes nothing. |
| [SC2037](https://www.shellcheck.net/wiki/SC2037) | error |  | yes | A command's output is meant to be assigned, but the value is a prefix assignment for the next word. |
| [SC2038](https://www.shellcheck.net/wiki/SC2038) | warning |  | yes | Piping find output to xargs breaks on file names with spaces or newlines. |
| [SC2041](https://www.shellcheck.net/wiki/SC2041) | warning |  | yes | A single-quoted loop item is one literal word, not a command whose output is iterated. |
| [SC2042](https://www.shellcheck.net/wiki/SC2042) | warning |  | yes | Loop items are separated by spaces, not commas. |
| [SC2043](https://www.shellcheck.net/wiki/SC2043) | warning |  | yes | A loop over a single constant item runs exactly once. |
| [SC2044](https://www.shellcheck.net/wiki/SC2044) | warning |  | yes | Iterating over find output breaks on file names with spaces or newlines. |
| [SC2045](https://www.shellcheck.net/wiki/SC2045) | error |  | yes | Iterating over ls output breaks on file names with spaces or newlines. |
| [SC2046](https://www.shellcheck.net/wiki/SC2046) | warning |  | yes | An unquoted command substitution is split into words and expanded as a glob. |
| [SC2048](https://www.shellcheck.net/wiki/SC2048) | warning |  | yes | An unquoted $@ or ${array\[@\]} joins the elements and splits them again. |
| [SC2049](https://www.shellcheck.net/wiki/SC2049) | warning |  | yes | =~ is given a glob, but it takes a regex. |
| [SC2050](https://www.shellcheck.net/wiki/SC2050) | warning |  | yes | Both sides of a comparison are constant, so the test always has the same result. |
| [SC2051](https://www.shellcheck.net/wiki/SC2051) | warning |  | yes | Braces are expanded before variables, so a range built from variables is not a range. |
| [SC2053](https://www.shellcheck.net/wiki/SC2053) | warning |  | yes | An unquoted right-hand side of == or != in \[\[ \]\] is matched as a glob pattern. |
| [SC2054](https://www.shellcheck.net/wiki/SC2054) | warning |  | yes | Array elements are separated by whitespace, so commas become part of the values. |
| [SC2055](https://www.shellcheck.net/wiki/SC2055) | warning |  | yes | A value always differs from at least one of two others, so a != ... -o != ... test is always true. |
| [SC2056](https://www.shellcheck.net/wiki/SC2056) | warning |  | yes | A value always differs from at least one of two others, so a != ... \|\| != ... list is always true. |
| [SC2057](https://www.shellcheck.net/wiki/SC2057) | warning |  | yes | A comparison uses an operator that test does not understand. |
| [SC2058](https://www.shellcheck.net/wiki/SC2058) | warning |  | yes | A test uses an operator that does not exist. |
| [SC2059](https://www.shellcheck.net/wiki/SC2059) | info |  | yes | A printf format contains an expansion, so data can be read as format directives. |
| [SC2060](https://www.shellcheck.net/wiki/SC2060) | warning |  | yes | An unquoted tr set may be expanded as a glob before tr sees it. |
| [SC2061](https://www.shellcheck.net/wiki/SC2061) | warning |  | yes | An unquoted find pattern may be expanded by the shell before find sees it. |
| [SC2062](https://www.shellcheck.net/wiki/SC2062) | warning |  | yes | An unquoted grep pattern may be expanded as a glob before grep sees it. |
| [SC2063](https://www.shellcheck.net/wiki/SC2063) | warning |  | yes | grep is given a glob, but it takes a regular expression. |
| [SC2064](https://www.shellcheck.net/wiki/SC2064) | warning |  | yes | An expansion in a trap handler is expanded when the trap is set, not when it fires. |
| [SC2065](https://www.shellcheck.net/wiki/SC2065) | warning |  | yes | \> or \< inside test is a file redirection, not a comparison. |
| [SC2066](https://www.shellcheck.net/wiki/SC2066) | error |  | yes | Double quotes make a loop list a single item, so the loop runs once. |
| [SC2067](https://www.shellcheck.net/wiki/SC2067) | error |  | yes | A find -exec action has no separate ; or + argument to end it. |
| [SC2068](https://www.shellcheck.net/wiki/SC2068) | error |  | yes | An unquoted array expansion splits its elements again. |
| [SC2069](https://www.shellcheck.net/wiki/SC2069) | warning |  | yes | 2\>&1 placed before a file redirection still points stderr at the old target. |
| [SC2070](https://www.shellcheck.net/wiki/SC2070) | error |  | yes | -n with an unquoted operand inside \[ \] is true even when the value is empty. |
| [SC2071](https://www.shellcheck.net/wiki/SC2071) | error |  | yes | A string comparison operator is used to compare numbers. |
| [SC2072](https://www.shellcheck.net/wiki/SC2072) | error |  | yes | Shell tests compare integers only, not decimals. |
| [SC2073](https://www.shellcheck.net/wiki/SC2073) | error |  | yes | An unescaped \< or \> inside \[ \] is a redirection, not a comparison. |
| [SC2074](https://www.shellcheck.net/wiki/SC2074) | error |  | yes | =~ is used inside \[ \], which has no regex matching. |
| [SC2075](https://www.shellcheck.net/wiki/SC2075) | error |  | yes | \< or \> is escaped or quoted inside \[\[ \]\], where it must not be. |
| [SC2076](https://www.shellcheck.net/wiki/SC2076) | warning |  | yes | A quoted right-hand side of =~ is matched literally instead of as a regex. |
| [SC2077](https://www.shellcheck.net/wiki/SC2077) | error |  | yes | A comparison without spaces around its operator only tests that the word is non-empty. |
| [SC2078](https://www.shellcheck.net/wiki/SC2078) | error |  | yes | A test of a constant string is always true. |
| [SC2079](https://www.shellcheck.net/wiki/SC2079) | error |  | yes | Shell arithmetic is integer only, so a decimal is not a valid number. |
| [SC2080](https://www.shellcheck.net/wiki/SC2080) | error |  | yes | A leading 0 makes a number octal, where 8 and 9 are not valid digits. |
| [SC2081](https://www.shellcheck.net/wiki/SC2081) | error |  | yes | \[ \] compares a glob pattern as literal text and never matches it. |
| [SC2082](https://www.shellcheck.net/wiki/SC2082) | error |  | yes | An expansion nested inside a parameter name does not compute the variable to expand. |
| [SC2083](https://www.shellcheck.net/wiki/SC2083) | error |  | yes | A space after ./ runs the current directory instead of the intended file. |
| [SC2084](https://www.shellcheck.net/wiki/SC2084) | error |  | yes | The result of an arithmetic expansion is run as a command. |
| [SC2086](https://www.shellcheck.net/wiki/SC2086) | info |  | yes | An unquoted expansion is split on whitespace and expanded as a glob. |
| [SC2087](https://www.shellcheck.net/wiki/SC2087) | warning |  | yes | A here-document sent to ssh is expanded locally before it is sent. |
| [SC2088](https://www.shellcheck.net/wiki/SC2088) | warning |  | yes | A quoted ~ is not expanded to the home directory. |
| [SC2089](https://www.shellcheck.net/wiki/SC2089) | warning |  | yes | Quotes or backslashes stored in a value do not group words when it is expanded. |
| [SC2090](https://www.shellcheck.net/wiki/SC2090) | warning |  | yes | Quotes or backslashes stored in a variable are passed on literally when it is expanded. |
| [SC2091](https://www.shellcheck.net/wiki/SC2091) | warning |  | yes | The output of a $( ) command substitution is run as a command. |
| [SC2092](https://www.shellcheck.net/wiki/SC2092) | warning |  | yes | The output of a backtick command substitution is run as a command. |
| [SC2093](https://www.shellcheck.net/wiki/SC2093) | warning |  | yes | exec replaces the shell, so the commands after it only run if exec fails. |
| [SC2094](https://www.shellcheck.net/wiki/SC2094) | info |  | yes | A pipeline reads and writes the same file, which the output redirection truncates first. |
| [SC2095](https://www.shellcheck.net/wiki/SC2095) | warning |  | yes | A command inside a while read loop reads stdin and may consume the loop's input. |
| [SC2096](https://www.shellcheck.net/wiki/SC2096) | error |  | yes | The kernel passes everything after the shebang interpreter as a single argument. |
| [SC2097](https://www.shellcheck.net/wiki/SC2097) | warning |  | yes | A prefix assignment only affects the command's environment, not expansions on the same line. |
| [SC2098](https://www.shellcheck.net/wiki/SC2098) | warning |  | yes | An expansion runs before a prefix assignment on the same line takes effect. |
| [SC2099](https://www.shellcheck.net/wiki/SC2099) | warning |  | yes | Arithmetic written as separate words in an assignment is not evaluated. |
| [SC2100](https://www.shellcheck.net/wiki/SC2100) | warning |  | yes | An arithmetic-looking value is assigned as a literal string. |
| [SC2101](https://www.shellcheck.net/wiki/SC2101) | warning |  | yes | A character class like \[:digit:\] is not inside a second pair of brackets. |
| [SC2102](https://www.shellcheck.net/wiki/SC2102) | info |  | yes | A bracket expression lists the same character twice, which suggests it was meant to match numbers. |
| [SC2103](https://www.shellcheck.net/wiki/SC2103) | info |  |  | Changing back with cd after commands can leave the script in the wrong directory if a cd fails. |
| [SC2104](https://www.shellcheck.net/wiki/SC2104) | error |  | yes | break or continue in a function does not leave the function. |
| [SC2105](https://www.shellcheck.net/wiki/SC2105) | error |  | yes | break or continue is used outside a loop. |
| [SC2106](https://www.shellcheck.net/wiki/SC2106) | warning |  | yes | break or continue in a subshell only exits the subshell, not the surrounding loop. |
| [SC2107](https://www.shellcheck.net/wiki/SC2107) | error |  | yes | && is used inside \[ \], where it cannot combine tests. |
| [SC2108](https://www.shellcheck.net/wiki/SC2108) | error |  | yes | -a does not combine tests inside \[\[ \]\]. |
| [SC2109](https://www.shellcheck.net/wiki/SC2109) | error |  | yes | \|\| is used inside \[ \], where it cannot combine tests. |
| [SC2110](https://www.shellcheck.net/wiki/SC2110) | error |  | yes | -o does not combine tests inside \[\[ \]\]. |
| [SC2111](https://www.shellcheck.net/wiki/SC2111) | error |  | yes | ksh does not accept both the function keyword and () in one definition. |
| [SC2112](https://www.shellcheck.net/wiki/SC2112) | warning |  | yes | The function keyword is not POSIX. |
| [SC2113](https://www.shellcheck.net/wiki/SC2113) | warning |  | yes | The function keyword is not POSIX. |
| [SC2114](https://www.shellcheck.net/wiki/SC2114) | warning |  | yes | A command removes a path recursively. |
| [SC2115](https://www.shellcheck.net/wiki/SC2115) | warning |  | yes | A removal path built from expansions removes a top-level path if they are empty. |
| [SC2116](https://www.shellcheck.net/wiki/SC2116) | style |  |  | echo only passes its arguments through to a command substitution. |
| [SC2117](https://www.shellcheck.net/wiki/SC2117) | info |  | yes | su starts an interactive shell, so the rest of the script waits until it exits. |
| [SC2118](https://www.shellcheck.net/wiki/SC2118) | error |  | yes | ksh reads \|& as starting a coprocess, not as a pipe of stdout and stderr. |
| [SC2119](https://www.shellcheck.net/wiki/SC2119) | info |  | yes | A function reads positional parameters, but this call passes none. |
| [SC2120](https://www.shellcheck.net/wiki/SC2120) | warning |  | yes | A function reads positional parameters, but no call passes any arguments. |
| [SC2121](https://www.shellcheck.net/wiki/SC2121) | warning |  | yes | set does not assign variables. |
| [SC2122](https://www.shellcheck.net/wiki/SC2122) | error |  | yes | \<= and \>= are not test operators. |
| [SC2123](https://www.shellcheck.net/wiki/SC2123) | warning |  | yes | PATH is replaced with something that is not a list of directories. |
| [SC2124](https://www.shellcheck.net/wiki/SC2124) | warning |  | yes | Assigning an array to a string joins its elements. |
| [SC2125](https://www.shellcheck.net/wiki/SC2125) | warning |  | yes | Globs and brace expansions are not expanded in assignments. |
| [SC2126](https://www.shellcheck.net/wiki/SC2126) | style |  |  | grep piped to wc -l counts matching lines that grep -c counts directly. |
| [SC2127](https://www.shellcheck.net/wiki/SC2127) | error |  | yes | A construct is used that only bash or ksh supports. |
| [SC2128](https://www.shellcheck.net/wiki/SC2128) | warning |  | yes | Expanding an array without an index gives only its first element. |
| [SC2129](https://www.shellcheck.net/wiki/SC2129) | style |  |  | Several commands in a row append to the same file, opening it each time. |
| [SC2139](https://www.shellcheck.net/wiki/SC2139) | warning |  | yes | An expansion in an alias body is expanded when the alias is defined, not when it is used. |
| [SC2140](https://www.shellcheck.net/wiki/SC2140) | warning |  | yes | Quotes inside a double-quoted word end one string and start another. |
| [SC2141](https://www.shellcheck.net/wiki/SC2141) | warning |  | yes | An escape like \\n inside a quoted IFS value is a literal backslash and letter, not a control character. |
| [SC2142](https://www.shellcheck.net/wiki/SC2142) | error |  | yes | Aliases cannot take positional parameters. |
| [SC2143](https://www.shellcheck.net/wiki/SC2143) | style |  |  | A test checks whether grep's output is empty instead of using its exit status. |
| [SC2144](https://www.shellcheck.net/wiki/SC2144) | error |  | yes | A file test operator is given a glob, which it does not expand into a list. |
| [SC2145](https://www.shellcheck.net/wiki/SC2145) | error |  | yes | Joining text with an array attaches the text only to its first and last elements. |
| [SC2146](https://www.shellcheck.net/wiki/SC2146) | warning |  | yes | A find action only applies to the last -o branch. |
| [SC2147](https://www.shellcheck.net/wiki/SC2147) | warning |  | yes | A quoted ~ in PATH is not expanded. |
| [SC2148](https://www.shellcheck.net/wiki/SC2148) | error |  | yes | Nothing names the shell the script targets. |
| [SC2150](https://www.shellcheck.net/wiki/SC2150) | warning |  | yes | find -exec runs a program directly, not a shell command line. |
| [SC2151](https://www.shellcheck.net/wiki/SC2151) | error |  | yes | return or exit is given more than one status. |
| [SC2152](https://www.shellcheck.net/wiki/SC2152) | error |  | yes | return or exit is given a status that is not an integer from 0 to 255. |
| [SC2153](https://www.shellcheck.net/wiki/SC2153) | info |  | yes | A variable is never assigned, but one with a similar name is. |
| [SC2154](https://www.shellcheck.net/wiki/SC2154) | warning |  | yes | A variable is read but never assigned in this script. |
| [SC2155](https://www.shellcheck.net/wiki/SC2155) | warning |  | yes | Declaring and assigning in one command masks the status of the command substitution. |
| [SC2156](https://www.shellcheck.net/wiki/SC2156) | warning |  | yes | find's {} is injected into the text of an inline script. |
| [SC2157](https://www.shellcheck.net/wiki/SC2157) | error |  | yes | A test operand always contains literal text, so the test always has the same result. |
| [SC2158](https://www.shellcheck.net/wiki/SC2158) | error |  | yes | \[ false \] only tests that the string false is non-empty, so it is always true. |
| [SC2159](https://www.shellcheck.net/wiki/SC2159) | error |  | yes | \[ 0 \] only tests that the string 0 is non-empty, so it is always true. |
| [SC2160](https://www.shellcheck.net/wiki/SC2160) | style |  |  | \[ true \] does the same as running true. |
| [SC2161](https://www.shellcheck.net/wiki/SC2161) | style |  |  | \[ 1 \] is always true. |
| [SC2162](https://www.shellcheck.net/wiki/SC2162) | info |  | yes | read without -r treats backslashes as escapes and drops them. |
| [SC2163](https://www.shellcheck.net/wiki/SC2163) | warning |  | yes | export of $name exports the variable named by its value, not the variable itself. |
| [SC2164](https://www.shellcheck.net/wiki/SC2164) | warning |  | yes | The commands after a failed cd run in the wrong directory. |
| [SC2165](https://www.shellcheck.net/wiki/SC2165) | warning |  | yes | A nested loop reuses the outer loop's variable. |
| [SC2166](https://www.shellcheck.net/wiki/SC2166) | warning |  | yes | -a or -o inside \[ \] is ambiguous and obsolescent. |
| [SC2167](https://www.shellcheck.net/wiki/SC2167) | warning |  | yes | A nested loop overwrites the outer loop's variable. |
| [SC2168](https://www.shellcheck.net/wiki/SC2168) | error |  | yes | local is used outside a function. |
| [SC2170](https://www.shellcheck.net/wiki/SC2170) | error |  | yes | A numeric comparison operator is given an operand that is not a number. |
| [SC2171](https://www.shellcheck.net/wiki/SC2171) | warning |  | yes | A closing bracket ends a test that was never opened. |
| [SC2172](https://www.shellcheck.net/wiki/SC2172) | warning |  | yes | Signal numbers differ between systems. |
| [SC2173](https://www.shellcheck.net/wiki/SC2173) | error |  | yes | SIGKILL and SIGSTOP cannot be trapped. |
| [SC2174](https://www.shellcheck.net/wiki/SC2174) | warning |  | yes | mkdir -p with -m only sets the mode of the deepest directory. |
| [SC2175](https://www.shellcheck.net/wiki/SC2175) | style |  |  | A brace range given to eval unquoted is expanded before the variable is substituted. |
| [SC2176](https://www.shellcheck.net/wiki/SC2176) | warning |  | yes | time on a pipeline is undefined in POSIX sh. |
| [SC2177](https://www.shellcheck.net/wiki/SC2177) | warning |  | yes | time on a compound command is undefined in POSIX sh. |
| [SC2178](https://www.shellcheck.net/wiki/SC2178) | warning |  | yes | Assigning a plain string to an array only replaces its first element. |
| [SC2179](https://www.shellcheck.net/wiki/SC2179) | warning |  | yes | Appending a string to an array changes its first element instead of adding one. |
| [SC2181](https://www.shellcheck.net/wiki/SC2181) | style |  |  | $? is inspected after a command instead of testing the command directly. |
| [SC2182](https://www.shellcheck.net/wiki/SC2182) | error |  | yes | A printf format has no conversion specifiers, so its arguments are ignored. |
| [SC2183](https://www.shellcheck.net/wiki/SC2183) | warning |  | yes | A printf format expects a different number of arguments than it is given. |
| [SC2184](https://www.shellcheck.net/wiki/SC2184) | warning |  | yes | An unquoted argument to unset may have its index brackets expanded as a glob. |
| [SC2185](https://www.shellcheck.net/wiki/SC2185) | info |  |  | find has no explicit starting path, which some versions require. |
| [SC2186](https://www.shellcheck.net/wiki/SC2186) | warning |  | yes | tempfile is deprecated and missing from many systems. |
| [SC2187](https://www.shellcheck.net/wiki/SC2187) | warning |  | yes | ash has no specification of its own, so the script is checked as dash. |
| [SC2188](https://www.shellcheck.net/wiki/SC2188) | warning |  | yes | A redirection stands alone without a command. |
| [SC2189](https://www.shellcheck.net/wiki/SC2189) | error |  | yes | A pipeline element is only a redirection, with no command to feed. |
| [SC2190](https://www.shellcheck.net/wiki/SC2190) | warning |  | yes | An element of an associative array has no key. |
| [SC2191](https://www.shellcheck.net/wiki/SC2191) | warning |  | yes | An = in an array element is literal text rather than part of \[index\]=value. |
| [SC2192](https://www.shellcheck.net/wiki/SC2192) | warning |  | yes | An array element has an index but no value. |
| [SC2193](https://www.shellcheck.net/wiki/SC2193) | warning |  | yes | No string can match both sides of a comparison, so it never succeeds. |
| [SC2194](https://www.shellcheck.net/wiki/SC2194) | warning |  | yes | The case word is a constant, so the same branch is taken every time. |
| [SC2195](https://www.shellcheck.net/wiki/SC2195) | warning |  | yes | A case pattern can never match the case word. |
| [SC2196](https://www.shellcheck.net/wiki/SC2196) | info |  |  | egrep is deprecated and missing from some systems. |
| [SC2197](https://www.shellcheck.net/wiki/SC2197) | info |  |  | fgrep is deprecated and missing from some systems. |
| [SC2198](https://www.shellcheck.net/wiki/SC2198) | warning |  | yes | \[ \] receives each element of an array as a separate word, which breaks the test. |
| [SC2199](https://www.shellcheck.net/wiki/SC2199) | error |  | yes | \[\[ \]\] joins an array expansion into one string. |
| [SC2200](https://www.shellcheck.net/wiki/SC2200) | warning |  | yes | Brace expansion turns a \[ \] operand into several words. |
| [SC2201](https://www.shellcheck.net/wiki/SC2201) | error |  | yes | Brace expansion does not happen inside \[\[ \]\]. |
| [SC2202](https://www.shellcheck.net/wiki/SC2202) | warning |  | yes | A glob in a \[ \] operand can expand to several words. |
| [SC2203](https://www.shellcheck.net/wiki/SC2203) | error |  | yes | \[\[ \]\] does not expand a glob outside the right side of = and !=. |
| [SC2204](https://www.shellcheck.net/wiki/SC2204) | error |  | yes | ( ) runs a subshell, not a test. |
| [SC2205](https://www.shellcheck.net/wiki/SC2205) | warning |  | yes | ( ) runs a subshell, not a comparison. |
| [SC2206](https://www.shellcheck.net/wiki/SC2206) | warning |  | yes | An unquoted expansion is split into words and globbed when building an array. |
| [SC2207](https://www.shellcheck.net/wiki/SC2207) | warning |  | yes | Command output is split on whitespace and globbed when building an array. |
| [SC2208](https://www.shellcheck.net/wiki/SC2208) | error |  | yes | The variable name given to -v is expanded as a glob. |
| [SC2209](https://www.shellcheck.net/wiki/SC2209) | warning |  | yes | A command name is assigned as a string instead of its output. |
| [SC2210](https://www.shellcheck.net/wiki/SC2210) | warning |  | yes | A redirection to a number writes to or reads from a file with that name. |
| [SC2211](https://www.shellcheck.net/wiki/SC2211) | warning |  | yes | A glob is used as a command name. |
| [SC2212](https://www.shellcheck.net/wiki/SC2212) | style |  |  | An empty test is always false. |
| [SC2213](https://www.shellcheck.net/wiki/SC2213) | warning |  | yes | getopts accepts an option that the case statement has no branch for. |
| [SC2214](https://www.shellcheck.net/wiki/SC2214) | warning |  | yes | A case branch handles an option that is not in the getopts string. |
| [SC2215](https://www.shellcheck.net/wiki/SC2215) | warning |  | yes | A word that looks like an option is used as a command name. |
| [SC2216](https://www.shellcheck.net/wiki/SC2216) | warning |  | yes | A command that does not read standard input is piped data. |
| [SC2217](https://www.shellcheck.net/wiki/SC2217) | warning |  | yes | A command that does not read standard input is given redirected input. |
| [SC2218](https://www.shellcheck.net/wiki/SC2218) | error |  | yes | A function is called before the line that defines it runs. |
| [SC2219](https://www.shellcheck.net/wiki/SC2219) | style |  |  | let is used where an arithmetic command needs no quoting. |
| [SC2220](https://www.shellcheck.net/wiki/SC2220) | warning |  | yes | A case statement handling getopts options has no \*) branch for invalid options. |
| [SC2221](https://www.shellcheck.net/wiki/SC2221) | warning |  | yes | A case pattern also matches everything a later pattern matches, so that one is never reached. |
| [SC2222](https://www.shellcheck.net/wiki/SC2222) | warning |  | yes | A case pattern never matches because an earlier pattern catches everything it would. |
| [SC2223](https://www.shellcheck.net/wiki/SC2223) | info |  | yes | An unquoted ${var:=default} is globbed on every run. |
| [SC2224](https://www.shellcheck.net/wiki/SC2224) | error |  | yes | mv is given only one path and no destination. |
| [SC2225](https://www.shellcheck.net/wiki/SC2225) | error |  | yes | cp is given only one path and no destination. |
| [SC2226](https://www.shellcheck.net/wiki/SC2226) | warning |  | yes | ln is given only one path and no link name. |
| [SC2227](https://www.shellcheck.net/wiki/SC2227) | warning |  | yes | A redirection on find applies to find itself, not to the command run by -exec. |
| [SC2229](https://www.shellcheck.net/wiki/SC2229) | warning |  | yes | read $name reads into the variable named by the value, not the variable itself. |
| [SC2230](https://www.shellcheck.net/wiki/SC2230) | info | `deprecate-which` |  | which is not standard and behaves differently between systems. |
| [SC2231](https://www.shellcheck.net/wiki/SC2231) | info |  | yes | An unquoted expansion next to a glob is split and globbed along with it. |
| [SC2232](https://www.shellcheck.net/wiki/SC2232) | warning |  | yes | sudo or doas is given a shell builtin, which has no program to run. |
| [SC2233](https://www.shellcheck.net/wiki/SC2233) | style |  |  | Parentheses around a condition start a needless subshell. |
| [SC2234](https://www.shellcheck.net/wiki/SC2234) | style |  |  | Parentheses around a test start a needless subshell. |
| [SC2235](https://www.shellcheck.net/wiki/SC2235) | style |  |  | Tests are grouped with ( ), which starts a needless subshell. |
| [SC2236](https://www.shellcheck.net/wiki/SC2236) | style | `avoid-negated-conditions` |  | A negated test operator is written as ! op instead of its opposite. |
| [SC2237](https://www.shellcheck.net/wiki/SC2237) | style | `avoid-negated-conditions` |  | A negated \[ \] or \[\[ \]\] test is written as ! instead of using the opposite operator. |
| [SC2238](https://www.shellcheck.net/wiki/SC2238) | warning |  | yes | A redirection names a file that is also a command, which suggests a pipe was meant. |
| [SC2239](https://www.shellcheck.net/wiki/SC2239) | error |  | yes | The shebang interpreter is not an absolute path. |
| [SC2240](https://www.shellcheck.net/wiki/SC2240) | warning |  | yes | Arguments after the file given to . are not passed to it in sh. |
| [SC2241](https://www.shellcheck.net/wiki/SC2241) | error |  | yes | exit is given more than one status. |
| [SC2242](https://www.shellcheck.net/wiki/SC2242) | error |  | yes | exit is given a status that is not an integer from 0 to 255. |
| [SC2243](https://www.shellcheck.net/wiki/SC2243) | style | `avoid-nullary-conditions` |  | A test of a command substitution relies on the implicit non-empty check. |
| [SC2244](https://www.shellcheck.net/wiki/SC2244) | style | `avoid-nullary-conditions` |  | A test of a string relies on the implicit non-empty check instead of -n. |
| [SC2245](https://www.shellcheck.net/wiki/SC2245) | warning |  | yes | ksh applies a file test operator only to the first match of a glob. |
| [SC2246](https://www.shellcheck.net/wiki/SC2246) | error |  | yes | The shebang interpreter ends in a slash, so it names a directory. |
| [SC2247](https://www.shellcheck.net/wiki/SC2247) | warning |  | yes | $" starts a translated string where a quoted substitution was meant. |
| [SC2248](https://www.shellcheck.net/wiki/SC2248) | style | `quote-safe-variables` |  | A variable that holds no spaces or globs today is expanded unquoted. |
| [SC2249](https://www.shellcheck.net/wiki/SC2249) | info | `add-default-case` |  | A case statement has no \*) branch for unexpected values. |
| [SC2250](https://www.shellcheck.net/wiki/SC2250) | style | `require-variable-braces` |  | A variable is expanded without braces. |
| [SC2251](https://www.shellcheck.net/wiki/SC2251) | info |  | yes | A negated command never triggers set -e. |
| [SC2252](https://www.shellcheck.net/wiki/SC2252) | warning |  | yes | A value always differs from at least one of several constants, so a \|\| chain of != tests is always true. |
| [SC2253](https://www.shellcheck.net/wiki/SC2253) | warning |  | yes | chmod -r removes read permission instead of changing modes recursively. |
| [SC2254](https://www.shellcheck.net/wiki/SC2254) | warning |  | yes | An unquoted expansion in a case pattern is matched as a glob. |
| [SC2255](https://www.shellcheck.net/wiki/SC2255) | error |  | yes | A numeric operand in \[ \] is an unquoted glob, not a number. |
| [SC2256](https://www.shellcheck.net/wiki/SC2256) | warning |  | yes | $"name" is a translated string, not an expansion of the variable. |
| [SC2257](https://www.shellcheck.net/wiki/SC2257) | warning |  | yes | Arithmetic that modifies a variable in a redirection may run in a subshell or not at all. |
| [SC2258](https://www.shellcheck.net/wiki/SC2258) | warning |  | yes | A trailing comma becomes part of a loop item. |
| [SC2259](https://www.shellcheck.net/wiki/SC2259) | error |  | yes | An input redirection replaces the piped input, discarding the previous command's output. |
| [SC2260](https://www.shellcheck.net/wiki/SC2260) | error |  | yes | An output redirection takes the output away from the pipe, so the next command reads nothing. |
| [SC2261](https://www.shellcheck.net/wiki/SC2261) | error |  | yes | Several redirections compete for the same file descriptor, and only the last takes effect. |
| [SC2262](https://www.shellcheck.net/wiki/SC2262) | warning |  | yes | An alias is used in the same parsing unit that defines it, where it does not take effect yet. |
| [SC2263](https://www.shellcheck.net/wiki/SC2263) | info |  | yes | A command runs as a program instead of the alias defined in the same parsing unit. |
| [SC2264](https://www.shellcheck.net/wiki/SC2264) | error |  | yes | A function that wraps a command calls itself unconditionally and recurses forever. |
| [SC2265](https://www.shellcheck.net/wiki/SC2265) | error |  | yes | A single & between tests runs the first in the background instead of combining them. |
| [SC2266](https://www.shellcheck.net/wiki/SC2266) | warning |  | yes | A single \| between tests pipes between them instead of combining them. |
| [SC2267](https://www.shellcheck.net/wiki/SC2267) | info |  |  | xargs -i is deprecated. |
| [SC2268](https://www.shellcheck.net/wiki/SC2268) | style |  |  | The x prefix in comparisons is an obsolete workaround. |
| [SC2269](https://www.shellcheck.net/wiki/SC2269) | info |  |  | A variable is assigned its own value, which does nothing. |
| [SC2270](https://www.shellcheck.net/wiki/SC2270) | error |  | yes | Positional parameters cannot be assigned with =. |
| [SC2271](https://www.shellcheck.net/wiki/SC2271) | error |  | yes | A variable name computed from an expansion cannot be assigned with =. |
| [SC2272](https://www.shellcheck.net/wiki/SC2272) | error |  | yes | A command name contains ==, which suggests a comparison or assignment was meant. |
| [SC2273](https://www.shellcheck.net/wiki/SC2273) | error |  | yes | A line of equals signs looks like a leftover merge conflict marker or text border. |
| [SC2274](https://www.shellcheck.net/wiki/SC2274) | error |  | yes | A command cannot start with ===. |
| [SC2275](https://www.shellcheck.net/wiki/SC2275) | error |  | yes | A command cannot start with =. |
| [SC2276](https://www.shellcheck.net/wiki/SC2276) | error |  | yes | A word with = is run as a command because the part before = is not a valid variable name. |
| [SC2277](https://www.shellcheck.net/wiki/SC2277) | error |  | yes | $0 cannot be assigned like a variable in bash. |
| [SC2278](https://www.shellcheck.net/wiki/SC2278) | error |  | yes | $0 cannot be assigned in ksh. |
| [SC2279](https://www.shellcheck.net/wiki/SC2279) | error |  | yes | $0 cannot be assigned in this shell. |
| [SC2280](https://www.shellcheck.net/wiki/SC2280) | error |  | yes | $0 cannot be assigned in sh. |
| [SC2281](https://www.shellcheck.net/wiki/SC2281) | error |  | yes | The left side of an assignment has a $. |
| [SC2282](https://www.shellcheck.net/wiki/SC2282) | error |  | yes | Variable names cannot start with a digit, so the assignment runs as a command. |
| [SC2283](https://www.shellcheck.net/wiki/SC2283) | error |  | yes | Spaces around = turn an assignment into a command. |
| [SC2284](https://www.shellcheck.net/wiki/SC2284) | error |  | yes | == on its own is neither an assignment nor a command. |
| [SC2285](https://www.shellcheck.net/wiki/SC2285) | error |  | yes | Spaces around += turn an append into a command. |
| [SC2286](https://www.shellcheck.net/wiki/SC2286) | error |  | yes | The command name is an empty string. |
| [SC2287](https://www.shellcheck.net/wiki/SC2287) | error |  | yes | A command name ends in a slash, so it names a directory. |
| [SC2288](https://www.shellcheck.net/wiki/SC2288) | warning |  | yes | A command name ends in a character that suggests a stray character or a missing space. |
| [SC2289](https://www.shellcheck.net/wiki/SC2289) | error |  | yes | A command name contains a tab or a line break. |
| [SC2290](https://www.shellcheck.net/wiki/SC2290) | error |  | yes | A space before = or += in a declaration turns the assignment into a separate word. |
| [SC2291](https://www.shellcheck.net/wiki/SC2291) | info |  | yes | Unquoted runs of spaces between echo arguments collapse to a single space. |
| [SC2292](https://www.shellcheck.net/wiki/SC2292) | style | `require-double-brackets` |  | \[ \] is used for a test where \[\[ \]\] is available. |
| [SC2293](https://www.shellcheck.net/wiki/SC2293) | style |  |  | eval is given ${array\[@\]@Q}, although it joins its arguments with spaces anyway. |
| [SC2294](https://www.shellcheck.net/wiki/SC2294) | warning |  | yes | eval joins array elements into one string and re-splits it, losing their boundaries. |
| [SC2295](https://www.shellcheck.net/wiki/SC2295) | info |  | yes | An unquoted expansion inside a pattern is matched as a glob. |
| [SC2296](https://www.shellcheck.net/wiki/SC2296) | error |  | yes | A parameter expansion starts with a character no parameter name can start with. |
| [SC2297](https://www.shellcheck.net/wiki/SC2297) | error |  | yes | Double quotes are used inside ${ }. |
| [SC2298](https://www.shellcheck.net/wiki/SC2298) | error |  | yes | An expansion is used as the name in ${ }. |
| [SC2299](https://www.shellcheck.net/wiki/SC2299) | error |  | yes | Parameter expansions cannot be nested. |
| [SC2300](https://www.shellcheck.net/wiki/SC2300) | error |  | yes | Parameter expansion cannot operate on a command substitution. |
| [SC2301](https://www.shellcheck.net/wiki/SC2301) | error |  | yes | A parameter expansion starts with quotes or other unexpected syntax. |
| [SC2302](https://www.shellcheck.net/wiki/SC2302) | warning |  | yes | A loop iterates over an associative array's values but uses the loop variable as a key. |
| [SC2303](https://www.shellcheck.net/wiki/SC2303) | warning |  | yes | A value taken from an associative array is used as a key. |
| [SC2304](https://www.shellcheck.net/wiki/SC2304) | error |  | yes | An unescaped \* given to expr is expanded as a glob. |
| [SC2305](https://www.shellcheck.net/wiki/SC2305) | warning |  | yes | An unquoted expr regex may be expanded as a glob. |
| [SC2306](https://www.shellcheck.net/wiki/SC2306) | warning |  | yes | Unescaped glob characters in expr arguments may be expanded by the shell. |
| [SC2307](https://www.shellcheck.net/wiki/SC2307) | warning |  | yes | expr is given too few arguments to form an expression. |
| [SC2308](https://www.shellcheck.net/wiki/SC2308) | info |  |  | An expr keyword is not specified by POSIX. |
| [SC2309](https://www.shellcheck.net/wiki/SC2309) | warning |  | yes | A numeric operator inside \[\[ \]\] evaluates its operand as arithmetic. |
| [SC2310](https://www.shellcheck.net/wiki/SC2310) | info | `check-set-e-suppressed` |  | set -e has no effect inside a function invoked as a condition. |
| [SC2311](https://www.shellcheck.net/wiki/SC2311) | info | `check-set-e-suppressed` |  | set -e has no effect inside a command substitution that does not inherit it. |
| [SC2312](https://www.shellcheck.net/wiki/SC2312) | info | `check-extra-masked-returns` |  | A command's exit status is masked by its context. |
| [SC2313](https://www.shellcheck.net/wiki/SC2313) | warning |  | yes | An unquoted array index in a read target may be expanded as a glob. |
| [SC2314](https://www.shellcheck.net/wiki/SC2314) | error |  | yes | Bats ignores the status of a negated command, so the line cannot fail the test. |
| [SC2315](https://www.shellcheck.net/wiki/SC2315) | error |  | yes | Bats ignores the status of a negated test, so the line cannot fail the test. |
| [SC2316](https://www.shellcheck.net/wiki/SC2316) | error |  | yes | A word given to a declaration command is parsed as a variable name, not as a modifier. |
| [SC2317](https://www.shellcheck.net/wiki/SC2317) | info |  |  | A command can never run, because every path to it exits, returns or loops forever first. |
| [SC2318](https://www.shellcheck.net/wiki/SC2318) | warning |  | yes | A variable assigned in a declaration is read again in the same declaration before it takes effect. |
| [SC2319](https://www.shellcheck.net/wiki/SC2319) | warning |  | yes | $? after a test is the status of that test, not of the command it checked. |
| [SC2320](https://www.shellcheck.net/wiki/SC2320) | warning |  | yes | $? after echo is the status of echo, not of the command before it. |
| [SC2321](https://www.shellcheck.net/wiki/SC2321) | style |  |  | $(( )) around an array index is redundant, since indices are already arithmetic. |
| [SC2322](https://www.shellcheck.net/wiki/SC2322) | style |  |  | Doubled parentheses in arithmetic are redundant. |
| [SC2323](https://www.shellcheck.net/wiki/SC2323) | style |  |  | Parentheses around a whole arithmetic expression are redundant. |
| [SC2324](https://www.shellcheck.net/wiki/SC2324) | warning |  | yes | += on a plain variable appends text instead of adding a number. |
| [SC2325](https://www.shellcheck.net/wiki/SC2325) | error |  | yes | sh allows only one ! before a pipeline. |
| [SC2326](https://www.shellcheck.net/wiki/SC2326) | error |  | yes | ! cannot negate a single stage after a \|. |
| [SC2327](https://www.shellcheck.net/wiki/SC2327) | warning |  | yes | A command substitution is always empty because the command's output is redirected elsewhere. |
| [SC2328](https://www.shellcheck.net/wiki/SC2328) | error |  | yes | A redirection takes the output away from the command substitution it is in. |
| [SC2329](https://www.shellcheck.net/wiki/SC2329) | info |  |  | A function is never invoked, and the script always exits before it could be sourced and called. |
| [SC2331](https://www.shellcheck.net/wiki/SC2331) | style |  |  | -a as a file test is a legacy synonym of -e that is easily confused with the -a conjunction. |
| [SC2332](https://www.shellcheck.net/wiki/SC2332) | error |  | yes | \[ ! -o name \] or \[ ! -a file \] reads -o or -a as a binary operator and is always true. |
| [SC2333](https://www.shellcheck.net/wiki/SC2333) | warning |  | yes | A value cannot equal two different constants at once, so the condition is always false. |
| [SC2334](https://www.shellcheck.net/wiki/SC2334) | warning |  | yes | A value cannot equal two different numbers at once, so the && is always false. |
| [SC2335](https://www.shellcheck.net/wiki/SC2335) | style | `avoid-negated-conditions` |  | A negated comparison is written as ! a op b instead of using the inverted operator. |
| [SC2336](https://www.shellcheck.net/wiki/SC2336) | warning |  | yes | cp -r is implementation-defined for special files. |
| [SC2337](https://www.shellcheck.net/wiki/SC2337) | warning |  | yes | A command that may exit before reading all its input can make the writer upstream fail with SIGPIPE under pipefail. |
| [SC2338](https://www.shellcheck.net/wiki/SC2338) | style | `require-double-equals` |  | = is used for string comparison in a bash test where == is preferred. |
| [SC3001](https://www.shellcheck.net/wiki/SC3001) | warning |  | yes | Process substitution is undefined in POSIX sh. |
| [SC3002](https://www.shellcheck.net/wiki/SC3002) | warning |  | yes | Extended globs are undefined in POSIX sh. |
| [SC3004](https://www.shellcheck.net/wiki/SC3004) | warning |  | yes | Locale-translated $"..." strings are undefined in POSIX sh. |
| [SC3005](https://www.shellcheck.net/wiki/SC3005) | warning |  | yes | C-style for ((...)) loops are undefined in POSIX sh. |
| [SC3006](https://www.shellcheck.net/wiki/SC3006) | warning |  | yes | The arithmetic command ((...)) is undefined in POSIX sh. |
| [SC3007](https://www.shellcheck.net/wiki/SC3007) | warning |  | yes | $\[...\] arithmetic is undefined in POSIX sh. |
| [SC3008](https://www.shellcheck.net/wiki/SC3008) | warning |  | yes | select loops are undefined in POSIX sh. |
| [SC3009](https://www.shellcheck.net/wiki/SC3009) | warning |  | yes | Brace expansion is undefined in POSIX sh. |
| [SC3010](https://www.shellcheck.net/wiki/SC3010) | warning |  | yes | \[\[ \]\] tests are undefined in POSIX sh and dash. |
| [SC3011](https://www.shellcheck.net/wiki/SC3011) | warning |  | yes | Here-strings with \<\<\< are undefined in POSIX sh. |
| [SC3012](https://www.shellcheck.net/wiki/SC3012) | warning |  | yes | Lexicographic comparison with \< or \> in \[ \] is undefined in POSIX sh. |
| [SC3014](https://www.shellcheck.net/wiki/SC3014) | warning |  | yes | == in \[ \] is undefined in POSIX sh and dash. |
| [SC3015](https://www.shellcheck.net/wiki/SC3015) | warning |  | yes | Regex matching with =~ is undefined in POSIX sh. |
| [SC3016](https://www.shellcheck.net/wiki/SC3016) | warning |  | yes | test -v is undefined in POSIX sh. |
| [SC3017](https://www.shellcheck.net/wiki/SC3017) | warning |  | yes | The unary test -a is undefined in POSIX sh. |
| [SC3018](https://www.shellcheck.net/wiki/SC3018) | warning |  | yes | The ++ and -- operators are undefined in POSIX sh. |
| [SC3019](https://www.shellcheck.net/wiki/SC3019) | warning |  | yes | Exponentiation with \*\* is undefined in POSIX sh. |
| [SC3020](https://www.shellcheck.net/wiki/SC3020) | warning |  | yes | Redirecting both streams with &\> is undefined in POSIX sh and dash. |
| [SC3021](https://www.shellcheck.net/wiki/SC3021) | warning |  | yes | Redirecting both streams to a file with \>& is undefined in POSIX sh. |
| [SC3022](https://www.shellcheck.net/wiki/SC3022) | warning |  | yes | Named file descriptors like {fd}\> are undefined in POSIX sh. |
| [SC3023](https://www.shellcheck.net/wiki/SC3023) | warning |  | yes | File descriptor numbers above 9 are undefined in POSIX sh. |
| [SC3024](https://www.shellcheck.net/wiki/SC3024) | warning |  | yes | Appending with += is undefined in POSIX sh. |
| [SC3025](https://www.shellcheck.net/wiki/SC3025) | warning |  | yes | Network pseudo-devices such as /dev/tcp are undefined in POSIX sh. |
| [SC3026](https://www.shellcheck.net/wiki/SC3026) | warning |  | yes | Negating a bracket expression with ^ is undefined in POSIX sh. |
| [SC3028](https://www.shellcheck.net/wiki/SC3028) | warning |  | yes | A bash-specific variable is undefined in POSIX sh. |
| [SC3029](https://www.shellcheck.net/wiki/SC3029) | warning |  | yes | The \|& operator is undefined in POSIX sh. |
| [SC3030](https://www.shellcheck.net/wiki/SC3030) | warning |  | yes | Arrays are undefined in POSIX sh. |
| [SC3031](https://www.shellcheck.net/wiki/SC3031) | warning |  | yes | Redirecting to or from a glob is undefined in POSIX sh. |
| [SC3032](https://www.shellcheck.net/wiki/SC3032) | warning |  | yes | coproc is undefined in POSIX sh. |
| [SC3033](https://www.shellcheck.net/wiki/SC3033) | warning |  | yes | A function name that is not a valid identifier is undefined in POSIX sh. |
| [SC3034](https://www.shellcheck.net/wiki/SC3034) | warning |  | yes | Reading a file with $(\< file) is undefined in POSIX sh. |
| [SC3035](https://www.shellcheck.net/wiki/SC3035) | warning |  | yes | Reading a file with \`\< file\` is undefined in POSIX sh. |
| [SC3036](https://www.shellcheck.net/wiki/SC3036) | error |  | yes | echo in dash and busybox sh does not support this flag. |
| [SC3037](https://www.shellcheck.net/wiki/SC3037) | warning |  | yes | echo flags are undefined in POSIX sh. |
| [SC3038](https://www.shellcheck.net/wiki/SC3038) | warning |  | yes | exec options are undefined in POSIX sh. |
| [SC3039](https://www.shellcheck.net/wiki/SC3039) | warning |  | yes | let is undefined in POSIX sh. |
| [SC3040](https://www.shellcheck.net/wiki/SC3040) | warning |  | yes | This set -o option is undefined in POSIX sh. |
| [SC3041](https://www.shellcheck.net/wiki/SC3041) | warning |  | yes | This set flag is undefined in POSIX sh. |
| [SC3042](https://www.shellcheck.net/wiki/SC3042) | warning |  | yes | This set option is undefined in POSIX sh. |
| [SC3043](https://www.shellcheck.net/wiki/SC3043) | warning |  | yes | local is undefined in POSIX sh. |
| [SC3044](https://www.shellcheck.net/wiki/SC3044) | warning |  | yes | A bash builtin is used that is undefined in POSIX sh. |
| [SC3045](https://www.shellcheck.net/wiki/SC3045) | warning |  | yes | A builtin option is used that is undefined in POSIX sh. |
| [SC3046](https://www.shellcheck.net/wiki/SC3046) | warning |  | yes | source is undefined in POSIX sh and dash. |
| [SC3047](https://www.shellcheck.net/wiki/SC3047) | warning |  | yes | Trapping this signal or condition is undefined in POSIX sh. |
| [SC3048](https://www.shellcheck.net/wiki/SC3048) | warning |  | yes | The SIG prefix on a signal name is undefined in POSIX sh and dash. |
| [SC3049](https://www.shellcheck.net/wiki/SC3049) | warning |  | yes | Lowercase signal names are undefined in POSIX sh. |
| [SC3050](https://www.shellcheck.net/wiki/SC3050) | warning |  | yes | printf %q is undefined in POSIX sh. |
| [SC3051](https://www.shellcheck.net/wiki/SC3051) | warning |  | yes | source is undefined in POSIX sh and dash, also when the file it loads is followed. |
| [SC3052](https://www.shellcheck.net/wiki/SC3052) | warning |  | yes | Arithmetic base prefixes like 16# are undefined in POSIX sh. |
| [SC3053](https://www.shellcheck.net/wiki/SC3053) | warning |  | yes | Indirect expansion with ${!name} is undefined in POSIX sh. |
| [SC3054](https://www.shellcheck.net/wiki/SC3054) | warning |  | yes | Array subscripts are undefined in POSIX sh. |
| [SC3055](https://www.shellcheck.net/wiki/SC3055) | warning |  | yes | Listing array keys with ${!array\[@\]} is undefined in POSIX sh. |
| [SC3056](https://www.shellcheck.net/wiki/SC3056) | warning |  | yes | Listing variable names by prefix with ${!prefix\*} is undefined in POSIX sh. |
| [SC3057](https://www.shellcheck.net/wiki/SC3057) | warning |  | yes | Substring expansion ${var:offset} is undefined in POSIX sh and dash. |
| [SC3058](https://www.shellcheck.net/wiki/SC3058) | warning |  | yes | Applying a length or trim operator to $@ or $\* is undefined in POSIX sh and dash. |
| [SC3059](https://www.shellcheck.net/wiki/SC3059) | warning |  | yes | Case modification in parameter expansion is undefined in POSIX sh. |
| [SC3060](https://www.shellcheck.net/wiki/SC3060) | warning |  | yes | Pattern substitution ${var/pattern/replacement} is undefined in POSIX sh and dash. |
| [SC3061](https://www.shellcheck.net/wiki/SC3061) | warning |  | yes | read without a variable name is undefined in POSIX sh. |
| [SC3062](https://www.shellcheck.net/wiki/SC3062) | warning |  | yes | Checking a shell option with test -o is undefined in POSIX sh. |
| [SC3063](https://www.shellcheck.net/wiki/SC3063) | warning |  | yes | test -R is undefined in POSIX sh. |
| [SC3064](https://www.shellcheck.net/wiki/SC3064) | warning |  | yes | test -N is undefined in POSIX sh. |
| [SC3065](https://www.shellcheck.net/wiki/SC3065) | warning |  | yes | test -k is undefined in POSIX sh. |
| [SC3066](https://www.shellcheck.net/wiki/SC3066) | warning |  | yes | test -G is undefined in POSIX sh. |
| [SC3067](https://www.shellcheck.net/wiki/SC3067) | warning |  | yes | test -O is undefined in POSIX sh. |
| [SL1001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1001) | warning |  | yes | eval runs untrusted data as code. |
| [SL1002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1002) | warning |  | yes | A shell -c script is built from untrusted data. |
| [SL1003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1003) | warning | `arithmetic-injection` | yes | Untrusted data is evaluated as arithmetic. |
| [SL1004](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1004) | warning |  | yes | A sourced path is built from untrusted data. |
| [SL1005](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1005) | warning |  | yes | Downloaded code runs unchecked. |
| [SL2001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2001) | style |  |  | A function in a script is never called. |
| [SL2002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2002) | warning |  | yes | An \`@\` transformation the shell does not know. |
| [SL2003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2003) | error |  | yes | A declaration assigns a name that is not a variable name. |
| [SL2004](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2004) | warning |  | yes | A trap is set inside a subshell. |
| [SL2005](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2005) | warning |  | yes | exit only leaves a subshell and nobody checks its status. |
| [SL2006](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2006) | info |  | yes | A temporary file or directory is never removed. |
| [SL2007](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2007) | info |  | yes | A function runs pushd but never popd. |
| [SL2008](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2008) | error |  | yes | A \`${…}\` the shell rejects when it runs it. |
| [SL2009](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2009) | info | `unused-directives` |  | A disabled check that nothing here reports. |
| [SL3001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3001) | error |  | yes | An awk program does not parse. |
| [SL3002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3002) | error |  | yes | A sed script does not parse. |
| [SL3003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3003) | error |  | yes | A jq filter does not parse. |
