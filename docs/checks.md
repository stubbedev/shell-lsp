# Checks

Every check the linter runs, generated from the lint catalog by
`go test ./internal/lint -run TestChecksDoc -update`. `SC` codes reuse ShellCheck's numbering;
`SL` codes are shell-lsp's own and are described in [rules.md](rules.md). Optional checks run only
when switched on by name with `enable=`, `--enable` or the `lint.optional` setting.

| Code | Severity | Optional | Summary |
| --- | --- | --- | --- |
| [SC1001](https://www.shellcheck.net/wiki/SC1001) | info |  | A backslash before an ordinary character outside quotes does nothing. |
| [SC1003](https://www.shellcheck.net/wiki/SC1003) | info |  | A backslash cannot escape a single quote inside single quotes, so the quote ends the string. |
| [SC1007](https://www.shellcheck.net/wiki/SC1007) | warning |  | A space after = ends the assignment, so the variable is set to empty for the next command only. |
| [SC1008](https://www.shellcheck.net/wiki/SC1008) | error |  | The shebang names an interpreter that is not a shell this linter recognises. |
| [SC1010](https://www.shellcheck.net/wiki/SC1010) | warning |  | A keyword is read as a plain word because no ; or line break comes before it. |
| [SC1011](https://www.shellcheck.net/wiki/SC1011) | warning |  | An apostrophe inside single quotes closes the string, leaving the rest outside the quotes. |
| [SC1012](https://www.shellcheck.net/wiki/SC1012) | warning |  | An escape like \\n or \\t outside quotes is just the letter, not a control character. |
| [SC1014](https://www.shellcheck.net/wiki/SC1014) | warning |  | Brackets around a command test the command's name as a string instead of running it. |
| [SC1017](https://www.shellcheck.net/wiki/SC1017) | error |  | The file contains a literal carriage return, usually from Windows line endings. |
| [SC1018](https://www.shellcheck.net/wiki/SC1018) | error |  | A Unicode space is not a plain space, so the shell does not split words on it. |
| [SC1019](https://www.shellcheck.net/wiki/SC1019) | error |  | A unary test operator has no operand. |
| [SC1020](https://www.shellcheck.net/wiki/SC1020) | error |  | A \] glued to the previous word is part of that word and does not end the test. |
| [SC1021](https://www.shellcheck.net/wiki/SC1021) | error |  | An escaped \\) glued to the previous word does not close the group. |
| [SC1026](https://www.shellcheck.net/wiki/SC1026) | error |  | A nested \[ does not group conditions inside \[ \]. |
| [SC1027](https://www.shellcheck.net/wiki/SC1027) | error |  | A comparison operator has no right-hand operand. |
| [SC1034](https://www.shellcheck.net/wiki/SC1034) | error |  | A test opens with a single \[ but closes with \]\]. |
| [SC1035](https://www.shellcheck.net/wiki/SC1035) | error |  | A test bracket needs a space between it and the next word. |
| [SC1037](https://www.shellcheck.net/wiki/SC1037) | error |  | $ followed by several digits expands only the first digit as a positional parameter. |
| [SC1039](https://www.shellcheck.net/wiki/SC1039) | error |  | An indented terminator does not end the here-document. |
| [SC1040](https://www.shellcheck.net/wiki/SC1040) | error |  | A terminator indented with spaces does not end a \<\<- here-document, which strips only tabs. |
| [SC1064](https://www.shellcheck.net/wiki/SC1064) | error |  | A function body is not a compound command. |
| [SC1071](https://www.shellcheck.net/wiki/SC1071) | error |  | A file run by an interpreter that is not a shell is not linted. |
| [SC1075](https://www.shellcheck.net/wiki/SC1075) | error |  | else if starts a nested if that needs its own fi; elif was probably meant. |
| [SC1076](https://www.shellcheck.net/wiki/SC1076) | error |  | \[ \] does not evaluate arithmetic, so an operator there is part of a word. |
| [SC1077](https://www.shellcheck.net/wiki/SC1077) | error |  | An acute accent is used where a backtick was meant. |
| [SC1078](https://www.shellcheck.net/wiki/SC1078) | warning |  | A quote that spans several lines closes right before more text, which suggests it is unbalanced. |
| [SC1079](https://www.shellcheck.net/wiki/SC1079) | info |  | A quote that looks like it opens a new string closes an earlier multi-line one. |
| [SC1080](https://www.shellcheck.net/wiki/SC1080) | error |  | A \[ test continues on the next line without a backslash before the line break. |
| [SC1081](https://www.shellcheck.net/wiki/SC1081) | error |  | Keywords are case sensitive, so a capitalised keyword runs as a command. |
| [SC1082](https://www.shellcheck.net/wiki/SC1082) | error |  | A UTF-8 byte order mark at the start of the file makes the shell misread the first line. |
| [SC1083](https://www.shellcheck.net/wiki/SC1083) | warning |  | A brace here is a literal character, not part of an expansion or a block. |
| [SC1084](https://www.shellcheck.net/wiki/SC1084) | error |  | The shebang is written !# instead of #!. |
| [SC1087](https://www.shellcheck.net/wiki/SC1087) | error |  | $name\[ expands the variable and appends a literal bracket instead of indexing the array. |
| [SC1090](https://www.shellcheck.net/wiki/SC1090) | warning |  | A sourced file is only known at run time, so it cannot be followed. |
| [SC1091](https://www.shellcheck.net/wiki/SC1091) | info |  | A sourced file was not found, so it is not followed. |
| [SC1094](https://www.shellcheck.net/wiki/SC1094) | warning |  | A sourced file has syntax errors, so what it defines may be missing. |
| [SC1097](https://www.shellcheck.net/wiki/SC1097) | error |  | == in an assignment assigns a value starting with = instead of comparing. |
| [SC1099](https://www.shellcheck.net/wiki/SC1099) | error |  | A # glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1100](https://www.shellcheck.net/wiki/SC1100) | error |  | A Unicode dash is used where a hyphen-minus test operator was meant. |
| [SC1101](https://www.shellcheck.net/wiki/SC1101) | error |  | A backslash escapes trailing whitespace instead of the line break, so the command ends there. |
| [SC1103](https://www.shellcheck.net/wiki/SC1103) | error |  | A shell directive names a shell this linter does not know. |
| [SC1104](https://www.shellcheck.net/wiki/SC1104) | error |  | A first line that looks like a shebang is missing its #. |
| [SC1107](https://www.shellcheck.net/wiki/SC1107) | warning |  | A directive uses a key that does not exist. |
| [SC1108](https://www.shellcheck.net/wiki/SC1108) | error |  | A test operator glued to its operand is part of the word, not a comparison. |
| [SC1109](https://www.shellcheck.net/wiki/SC1109) | error |  | An HTML entity copied from a web page is used instead of the character itself. |
| [SC1110](https://www.shellcheck.net/wiki/SC1110) | warning |  | A curly quote is a literal character, not shell quoting. |
| [SC1111](https://www.shellcheck.net/wiki/SC1111) | warning |  | A curly double quote is a literal character inside double quotes or a here-document. |
| [SC1112](https://www.shellcheck.net/wiki/SC1112) | warning |  | A curly single quote is a literal character inside single quotes. |
| [SC1113](https://www.shellcheck.net/wiki/SC1113) | error |  | A first line that looks like a shebang is missing its !. |
| [SC1114](https://www.shellcheck.net/wiki/SC1114) | error |  | Whitespace before #! keeps the shebang from working. |
| [SC1115](https://www.shellcheck.net/wiki/SC1115) | error |  | Whitespace between # and ! keeps the shebang from working. |
| [SC1118](https://www.shellcheck.net/wiki/SC1118) | error |  | A here-document terminator followed by whitespace does not end the here-document. |
| [SC1119](https://www.shellcheck.net/wiki/SC1119) | error |  | A here-document terminator followed by ) does not end the here-document. |
| [SC1120](https://www.shellcheck.net/wiki/SC1120) | error |  | A here-document terminator followed by a comment does not end the here-document. |
| [SC1121](https://www.shellcheck.net/wiki/SC1121) | error |  | A here-document terminator followed by an operator does not end the here-document. |
| [SC1122](https://www.shellcheck.net/wiki/SC1122) | error |  | A here-document terminator followed by more text does not end the here-document. |
| [SC1123](https://www.shellcheck.net/wiki/SC1123) | error |  | A directive stands in front of a keyword where it has no command to apply to. |
| [SC1124](https://www.shellcheck.net/wiki/SC1124) | error |  | A directive in front of a case branch pattern has no effect. |
| [SC1125](https://www.shellcheck.net/wiki/SC1125) | error |  | A disable list contains something that is not a check code or range. |
| [SC1126](https://www.shellcheck.net/wiki/SC1126) | error |  | A directive placed after a command's words or inside a continued list is rejected by ShellCheck. |
| [SC1127](https://www.shellcheck.net/wiki/SC1127) | error |  | A comment starts with // or a similar marker instead of #, so the line runs as a command. |
| [SC1128](https://www.shellcheck.net/wiki/SC1128) | error |  | A shebang that is not on the first line has no effect. |
| [SC1129](https://www.shellcheck.net/wiki/SC1129) | error |  | A ! glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1130](https://www.shellcheck.net/wiki/SC1130) | error |  | A : glued to a keyword makes one word, so the keyword is not recognised. |
| [SC1131](https://www.shellcheck.net/wiki/SC1131) | error |  | elsif, elseif or a similar spelling is not a keyword; the shell only has elif. |
| [SC1132](https://www.shellcheck.net/wiki/SC1132) | warning |  | An unquoted & inside a word, such as in a URL, sends the command to the background. |
| [SC1135](https://www.shellcheck.net/wiki/SC1135) | style |  | Closing quotes right after $ only to keep it literal is confusing. |
| [SC1139](https://www.shellcheck.net/wiki/SC1139) | error |  | -a or -o joins test commands outside \[ \], where it is just another word. |
| [SC1140](https://www.shellcheck.net/wiki/SC1140) | error |  | Words after the closing \] become arguments of the test. |
| [SC1143](https://www.shellcheck.net/wiki/SC1143) | error |  | A commented-out line ends a continued command, so the lines below run separately. |
| [SC1144](https://www.shellcheck.net/wiki/SC1144) | error |  | external-sources=true only takes effect in a .shellcheckrc file. |
| [SC1145](https://www.shellcheck.net/wiki/SC1145) | error |  | external-sources is set to something other than true or false. |
| [SC1146](https://www.shellcheck.net/wiki/SC1146) | error |  | extended-analysis is set to something other than true or false. |
| [SC2000](https://www.shellcheck.net/wiki/SC2000) | style |  | echo piped to wc counts a string's length that ${#var} gives directly. |
| [SC2001](https://www.shellcheck.net/wiki/SC2001) | style |  | sed runs on a single value that a ${var//pattern/replacement} expansion could handle. |
| [SC2002](https://www.shellcheck.net/wiki/SC2002) | style | `useless-use-of-cat` | cat only feeds a file to the next command, which could read it directly. |
| [SC2003](https://www.shellcheck.net/wiki/SC2003) | style |  | expr is an antiquated way to do arithmetic. |
| [SC2004](https://www.shellcheck.net/wiki/SC2004) | style |  | The $ on a variable inside arithmetic is unnecessary. |
| [SC2005](https://www.shellcheck.net/wiki/SC2005) | style |  | echo only repeats the output of a command substitution. |
| [SC2006](https://www.shellcheck.net/wiki/SC2006) | style |  | Backtick command substitution is legacy syntax that nests and escapes poorly. |
| [SC2007](https://www.shellcheck.net/wiki/SC2007) | style |  | $\[ \] is an obsolete arithmetic form. |
| [SC2009](https://www.shellcheck.net/wiki/SC2009) | info |  | Grepping ps output also matches the grep itself. |
| [SC2010](https://www.shellcheck.net/wiki/SC2010) | warning |  | Filtering ls output with grep breaks on unusual file names. |
| [SC2011](https://www.shellcheck.net/wiki/SC2011) | warning |  | Piping ls output to xargs breaks on file names with spaces. |
| [SC2012](https://www.shellcheck.net/wiki/SC2012) | info |  | Parsing ls output breaks on unusual file names. |
| [SC2013](https://www.shellcheck.net/wiki/SC2013) | info |  | A for loop over command output splits it into words, not lines. |
| [SC2014](https://www.shellcheck.net/wiki/SC2014) | info |  | An expansion in a find -exec command runs once before find starts, not once per file. |
| [SC2015](https://www.shellcheck.net/wiki/SC2015) | info |  | A && B \|\| C is not if-then-else: C also runs when A succeeds and B fails. |
| [SC2016](https://www.shellcheck.net/wiki/SC2016) | info |  | Single quotes keep $ and backticks literal, so nothing in them is expanded. |
| [SC2017](https://www.shellcheck.net/wiki/SC2017) | info |  | Integer division truncates before a later multiplication, losing precision. |
| [SC2018](https://www.shellcheck.net/wiki/SC2018) | info |  | The range a-z misses accented and other non-ASCII lowercase letters. |
| [SC2019](https://www.shellcheck.net/wiki/SC2019) | info |  | The range A-Z misses accented and other non-ASCII uppercase letters. |
| [SC2020](https://www.shellcheck.net/wiki/SC2020) | info |  | tr maps single characters, not words, so a repeated letter only uses its last mapping. |
| [SC2021](https://www.shellcheck.net/wiki/SC2021) | info |  | Brackets around a tr range are matched as literal characters. |
| [SC2022](https://www.shellcheck.net/wiki/SC2022) | info |  | In a regex, x\* repeats the character before it rather than matching anything after it. |
| [SC2023](https://www.shellcheck.net/wiki/SC2023) | info |  | The time keyword only accepts -p, so other options need /usr/bin/time. |
| [SC2024](https://www.shellcheck.net/wiki/SC2024) | warning |  | sudo or doas does not apply to a redirection, which the shell opens with the caller's permissions. |
| [SC2025](https://www.shellcheck.net/wiki/SC2025) | info |  | Terminal escape sequences in PS1 are not wrapped in \\\[ \\\], so bash miscounts the prompt's width. |
| [SC2026](https://www.shellcheck.net/wiki/SC2026) | info |  | A word between single-quoted strings is not quoted. |
| [SC2027](https://www.shellcheck.net/wiki/SC2027) | warning |  | Quotes inside a string close and reopen it, leaving the inner part unquoted. |
| [SC2028](https://www.shellcheck.net/wiki/SC2028) | info |  | echo does not reliably interpret backslash escapes. |
| [SC2029](https://www.shellcheck.net/wiki/SC2029) | info |  | An expansion in an ssh command runs on the client, not on the server. |
| [SC2030](https://www.shellcheck.net/wiki/SC2030) | info |  | A variable changed in a subshell loses the change when the subshell exits. |
| [SC2031](https://www.shellcheck.net/wiki/SC2031) | info |  | A variable read here still has its value from before a change made in a subshell. |
| [SC2032](https://www.shellcheck.net/wiki/SC2032) | info |  | A command that runs external programs is given a function this script defines. |
| [SC2033](https://www.shellcheck.net/wiki/SC2033) | warning |  | A command that runs external programs cannot see a shell function or alias. |
| [SC2034](https://www.shellcheck.net/wiki/SC2034) | warning |  | A variable is assigned but never read. |
| [SC2035](https://www.shellcheck.net/wiki/SC2035) | info |  | A file matched by a glob may start with a dash and be read as an option. |
| [SC2036](https://www.shellcheck.net/wiki/SC2036) | warning |  | A pipeline stage that only assigns a variable pipes nothing. |
| [SC2037](https://www.shellcheck.net/wiki/SC2037) | error |  | A command's output is meant to be assigned, but the value is a prefix assignment for the next word. |
| [SC2038](https://www.shellcheck.net/wiki/SC2038) | warning |  | Piping find output to xargs breaks on file names with spaces or newlines. |
| [SC2041](https://www.shellcheck.net/wiki/SC2041) | warning |  | A single-quoted loop item is one literal word, not a command whose output is iterated. |
| [SC2042](https://www.shellcheck.net/wiki/SC2042) | warning |  | Loop items are separated by spaces, not commas. |
| [SC2043](https://www.shellcheck.net/wiki/SC2043) | warning |  | A loop over a single constant item runs exactly once. |
| [SC2044](https://www.shellcheck.net/wiki/SC2044) | warning |  | Iterating over find output breaks on file names with spaces or newlines. |
| [SC2045](https://www.shellcheck.net/wiki/SC2045) | error |  | Iterating over ls output breaks on file names with spaces or newlines. |
| [SC2046](https://www.shellcheck.net/wiki/SC2046) | warning |  | An unquoted command substitution is split into words and expanded as a glob. |
| [SC2048](https://www.shellcheck.net/wiki/SC2048) | warning |  | An unquoted $@ or ${array\[@\]} joins the elements and splits them again. |
| [SC2049](https://www.shellcheck.net/wiki/SC2049) | warning |  | =~ is given a glob, but it takes a regex. |
| [SC2050](https://www.shellcheck.net/wiki/SC2050) | warning |  | Both sides of a comparison are constant, so the test always has the same result. |
| [SC2051](https://www.shellcheck.net/wiki/SC2051) | warning |  | Braces are expanded before variables, so a range built from variables is not a range. |
| [SC2053](https://www.shellcheck.net/wiki/SC2053) | warning |  | An unquoted right-hand side of == or != in \[\[ \]\] is matched as a glob pattern. |
| [SC2054](https://www.shellcheck.net/wiki/SC2054) | warning |  | Array elements are separated by whitespace, so commas become part of the values. |
| [SC2055](https://www.shellcheck.net/wiki/SC2055) | warning |  | A value always differs from at least one of two others, so a != ... -o != ... test is always true. |
| [SC2056](https://www.shellcheck.net/wiki/SC2056) | warning |  | A value always differs from at least one of two others, so a != ... \|\| != ... list is always true. |
| [SC2057](https://www.shellcheck.net/wiki/SC2057) | warning |  | A comparison uses an operator that test does not understand. |
| [SC2058](https://www.shellcheck.net/wiki/SC2058) | warning |  | A test uses an operator that does not exist. |
| [SC2059](https://www.shellcheck.net/wiki/SC2059) | info |  | A printf format contains an expansion, so data can be read as format directives. |
| [SC2060](https://www.shellcheck.net/wiki/SC2060) | warning |  | An unquoted tr set may be expanded as a glob before tr sees it. |
| [SC2061](https://www.shellcheck.net/wiki/SC2061) | warning |  | An unquoted find pattern may be expanded by the shell before find sees it. |
| [SC2062](https://www.shellcheck.net/wiki/SC2062) | warning |  | An unquoted grep pattern may be expanded as a glob before grep sees it. |
| [SC2063](https://www.shellcheck.net/wiki/SC2063) | warning |  | grep is given a glob, but it takes a regular expression. |
| [SC2064](https://www.shellcheck.net/wiki/SC2064) | warning |  | An expansion in a trap handler is expanded when the trap is set, not when it fires. |
| [SC2065](https://www.shellcheck.net/wiki/SC2065) | warning |  | \> or \< inside test is a file redirection, not a comparison. |
| [SC2066](https://www.shellcheck.net/wiki/SC2066) | error |  | Double quotes make a loop list a single item, so the loop runs once. |
| [SC2067](https://www.shellcheck.net/wiki/SC2067) | error |  | A find -exec action has no separate ; or + argument to end it. |
| [SC2068](https://www.shellcheck.net/wiki/SC2068) | error |  | An unquoted array expansion splits its elements again. |
| [SC2069](https://www.shellcheck.net/wiki/SC2069) | warning |  | 2\>&1 placed before a file redirection still points stderr at the old target. |
| [SC2070](https://www.shellcheck.net/wiki/SC2070) | error |  | -n with an unquoted operand inside \[ \] is true even when the value is empty. |
| [SC2071](https://www.shellcheck.net/wiki/SC2071) | error |  | A string comparison operator is used to compare numbers. |
| [SC2072](https://www.shellcheck.net/wiki/SC2072) | error |  | Shell tests compare integers only, not decimals. |
| [SC2073](https://www.shellcheck.net/wiki/SC2073) | error |  | An unescaped \< or \> inside \[ \] is a redirection, not a comparison. |
| [SC2074](https://www.shellcheck.net/wiki/SC2074) | error |  | =~ is used inside \[ \], which has no regex matching. |
| [SC2075](https://www.shellcheck.net/wiki/SC2075) | error |  | \< or \> is escaped or quoted inside \[\[ \]\], where it must not be. |
| [SC2076](https://www.shellcheck.net/wiki/SC2076) | warning |  | A quoted right-hand side of =~ is matched literally instead of as a regex. |
| [SC2077](https://www.shellcheck.net/wiki/SC2077) | error |  | A comparison without spaces around its operator only tests that the word is non-empty. |
| [SC2078](https://www.shellcheck.net/wiki/SC2078) | error |  | A test of a constant string is always true. |
| [SC2079](https://www.shellcheck.net/wiki/SC2079) | error |  | Shell arithmetic is integer only, so a decimal is not a valid number. |
| [SC2080](https://www.shellcheck.net/wiki/SC2080) | error |  | A leading 0 makes a number octal, where 8 and 9 are not valid digits. |
| [SC2081](https://www.shellcheck.net/wiki/SC2081) | error |  | \[ \] compares a glob pattern as literal text and never matches it. |
| [SC2082](https://www.shellcheck.net/wiki/SC2082) | error |  | An expansion nested inside a parameter name does not compute the variable to expand. |
| [SC2083](https://www.shellcheck.net/wiki/SC2083) | error |  | A space after ./ runs the current directory instead of the intended file. |
| [SC2084](https://www.shellcheck.net/wiki/SC2084) | error |  | The result of an arithmetic expansion is run as a command. |
| [SC2086](https://www.shellcheck.net/wiki/SC2086) | info |  | An unquoted expansion is split on whitespace and expanded as a glob. |
| [SC2087](https://www.shellcheck.net/wiki/SC2087) | warning |  | A here-document sent to ssh is expanded locally before it is sent. |
| [SC2088](https://www.shellcheck.net/wiki/SC2088) | warning |  | A quoted ~ is not expanded to the home directory. |
| [SC2089](https://www.shellcheck.net/wiki/SC2089) | warning |  | Quotes or backslashes stored in a value do not group words when it is expanded. |
| [SC2090](https://www.shellcheck.net/wiki/SC2090) | warning |  | Quotes or backslashes stored in a variable are passed on literally when it is expanded. |
| [SC2091](https://www.shellcheck.net/wiki/SC2091) | warning |  | The output of a $( ) command substitution is run as a command. |
| [SC2092](https://www.shellcheck.net/wiki/SC2092) | warning |  | The output of a backtick command substitution is run as a command. |
| [SC2093](https://www.shellcheck.net/wiki/SC2093) | warning |  | exec replaces the shell, so the commands after it only run if exec fails. |
| [SC2094](https://www.shellcheck.net/wiki/SC2094) | info |  | A pipeline reads and writes the same file, which the output redirection truncates first. |
| [SC2095](https://www.shellcheck.net/wiki/SC2095) | warning |  | A command inside a while read loop reads stdin and may consume the loop's input. |
| [SC2096](https://www.shellcheck.net/wiki/SC2096) | error |  | The kernel passes everything after the shebang interpreter as a single argument. |
| [SC2097](https://www.shellcheck.net/wiki/SC2097) | warning |  | A prefix assignment only affects the command's environment, not expansions on the same line. |
| [SC2098](https://www.shellcheck.net/wiki/SC2098) | warning |  | An expansion runs before a prefix assignment on the same line takes effect. |
| [SC2099](https://www.shellcheck.net/wiki/SC2099) | warning |  | Arithmetic written as separate words in an assignment is not evaluated. |
| [SC2100](https://www.shellcheck.net/wiki/SC2100) | warning |  | An arithmetic-looking value is assigned as a literal string. |
| [SC2101](https://www.shellcheck.net/wiki/SC2101) | warning |  | A character class like \[:digit:\] is not inside a second pair of brackets. |
| [SC2102](https://www.shellcheck.net/wiki/SC2102) | info |  | A bracket expression lists the same character twice, which suggests it was meant to match numbers. |
| [SC2103](https://www.shellcheck.net/wiki/SC2103) | info |  | Changing back with cd after commands can leave the script in the wrong directory if a cd fails. |
| [SC2104](https://www.shellcheck.net/wiki/SC2104) | error |  | break or continue in a function does not leave the function. |
| [SC2105](https://www.shellcheck.net/wiki/SC2105) | error |  | break or continue is used outside a loop. |
| [SC2106](https://www.shellcheck.net/wiki/SC2106) | warning |  | break or continue in a subshell only exits the subshell, not the surrounding loop. |
| [SC2107](https://www.shellcheck.net/wiki/SC2107) | error |  | && is used inside \[ \], where it cannot combine tests. |
| [SC2108](https://www.shellcheck.net/wiki/SC2108) | error |  | -a does not combine tests inside \[\[ \]\]. |
| [SC2109](https://www.shellcheck.net/wiki/SC2109) | error |  | \|\| is used inside \[ \], where it cannot combine tests. |
| [SC2110](https://www.shellcheck.net/wiki/SC2110) | error |  | -o does not combine tests inside \[\[ \]\]. |
| [SC2111](https://www.shellcheck.net/wiki/SC2111) | error |  | ksh does not accept both the function keyword and () in one definition. |
| [SC2112](https://www.shellcheck.net/wiki/SC2112) | warning |  | The function keyword is not POSIX. |
| [SC2113](https://www.shellcheck.net/wiki/SC2113) | warning |  | The function keyword is not POSIX. |
| [SC2114](https://www.shellcheck.net/wiki/SC2114) | warning |  | A command removes a path recursively. |
| [SC2115](https://www.shellcheck.net/wiki/SC2115) | warning |  | A removal path built from expansions removes a top-level path if they are empty. |
| [SC2116](https://www.shellcheck.net/wiki/SC2116) | style |  | echo only passes its arguments through to a command substitution. |
| [SC2117](https://www.shellcheck.net/wiki/SC2117) | info |  | su starts an interactive shell, so the rest of the script waits until it exits. |
| [SC2118](https://www.shellcheck.net/wiki/SC2118) | error |  | ksh reads \|& as starting a coprocess, not as a pipe of stdout and stderr. |
| [SC2119](https://www.shellcheck.net/wiki/SC2119) | info |  | A function reads positional parameters, but this call passes none. |
| [SC2120](https://www.shellcheck.net/wiki/SC2120) | warning |  | A function reads positional parameters, but no call passes any arguments. |
| [SC2121](https://www.shellcheck.net/wiki/SC2121) | warning |  | set does not assign variables. |
| [SC2122](https://www.shellcheck.net/wiki/SC2122) | error |  | \<= and \>= are not test operators. |
| [SC2123](https://www.shellcheck.net/wiki/SC2123) | warning |  | PATH is replaced with something that is not a list of directories. |
| [SC2124](https://www.shellcheck.net/wiki/SC2124) | warning |  | Assigning an array to a string joins its elements. |
| [SC2125](https://www.shellcheck.net/wiki/SC2125) | warning |  | Globs and brace expansions are not expanded in assignments. |
| [SC2126](https://www.shellcheck.net/wiki/SC2126) | style |  | grep piped to wc -l counts matching lines that grep -c counts directly. |
| [SC2127](https://www.shellcheck.net/wiki/SC2127) | error |  | A construct is used that only bash or ksh supports. |
| [SC2128](https://www.shellcheck.net/wiki/SC2128) | warning |  | Expanding an array without an index gives only its first element. |
| [SC2129](https://www.shellcheck.net/wiki/SC2129) | style |  | Several commands in a row append to the same file, opening it each time. |
| [SC2139](https://www.shellcheck.net/wiki/SC2139) | warning |  | An expansion in an alias body is expanded when the alias is defined, not when it is used. |
| [SC2140](https://www.shellcheck.net/wiki/SC2140) | warning |  | Quotes inside a double-quoted word end one string and start another. |
| [SC2141](https://www.shellcheck.net/wiki/SC2141) | warning |  | An escape like \\n inside a quoted IFS value is a literal backslash and letter, not a control character. |
| [SC2142](https://www.shellcheck.net/wiki/SC2142) | error |  | Aliases cannot take positional parameters. |
| [SC2143](https://www.shellcheck.net/wiki/SC2143) | style |  | A test checks whether grep's output is empty instead of using its exit status. |
| [SC2144](https://www.shellcheck.net/wiki/SC2144) | error |  | A file test operator is given a glob, which it does not expand into a list. |
| [SC2145](https://www.shellcheck.net/wiki/SC2145) | error |  | Joining text with an array attaches the text only to its first and last elements. |
| [SC2146](https://www.shellcheck.net/wiki/SC2146) | warning |  | A find action only applies to the last -o branch. |
| [SC2147](https://www.shellcheck.net/wiki/SC2147) | warning |  | A quoted ~ in PATH is not expanded. |
| [SC2148](https://www.shellcheck.net/wiki/SC2148) | error |  | Nothing names the shell the script targets. |
| [SC2150](https://www.shellcheck.net/wiki/SC2150) | warning |  | find -exec runs a program directly, not a shell command line. |
| [SC2151](https://www.shellcheck.net/wiki/SC2151) | error |  | return or exit is given more than one status. |
| [SC2152](https://www.shellcheck.net/wiki/SC2152) | error |  | return or exit is given a status that is not an integer from 0 to 255. |
| [SC2153](https://www.shellcheck.net/wiki/SC2153) | info |  | A variable is never assigned, but one with a similar name is. |
| [SC2154](https://www.shellcheck.net/wiki/SC2154) | warning |  | A variable is read but never assigned in this script. |
| [SC2155](https://www.shellcheck.net/wiki/SC2155) | warning |  | Declaring and assigning in one command masks the status of the command substitution. |
| [SC2156](https://www.shellcheck.net/wiki/SC2156) | warning |  | find's {} is injected into the text of an inline script. |
| [SC2157](https://www.shellcheck.net/wiki/SC2157) | error |  | A test operand always contains literal text, so the test always has the same result. |
| [SC2158](https://www.shellcheck.net/wiki/SC2158) | error |  | \[ false \] only tests that the string false is non-empty, so it is always true. |
| [SC2159](https://www.shellcheck.net/wiki/SC2159) | error |  | \[ 0 \] only tests that the string 0 is non-empty, so it is always true. |
| [SC2160](https://www.shellcheck.net/wiki/SC2160) | style |  | \[ true \] does the same as running true. |
| [SC2161](https://www.shellcheck.net/wiki/SC2161) | style |  | \[ 1 \] is always true. |
| [SC2162](https://www.shellcheck.net/wiki/SC2162) | info |  | read without -r treats backslashes as escapes and drops them. |
| [SC2163](https://www.shellcheck.net/wiki/SC2163) | warning |  | export of $name exports the variable named by its value, not the variable itself. |
| [SC2164](https://www.shellcheck.net/wiki/SC2164) | warning |  | The commands after a failed cd run in the wrong directory. |
| [SC2165](https://www.shellcheck.net/wiki/SC2165) | warning |  | A nested loop reuses the outer loop's variable. |
| [SC2166](https://www.shellcheck.net/wiki/SC2166) | warning |  | -a or -o inside \[ \] is ambiguous and obsolescent. |
| [SC2167](https://www.shellcheck.net/wiki/SC2167) | warning |  | A nested loop overwrites the outer loop's variable. |
| [SC2168](https://www.shellcheck.net/wiki/SC2168) | error |  | local is used outside a function. |
| [SC2170](https://www.shellcheck.net/wiki/SC2170) | error |  | A numeric comparison operator is given an operand that is not a number. |
| [SC2171](https://www.shellcheck.net/wiki/SC2171) | warning |  | A closing bracket ends a test that was never opened. |
| [SC2172](https://www.shellcheck.net/wiki/SC2172) | warning |  | Signal numbers differ between systems. |
| [SC2173](https://www.shellcheck.net/wiki/SC2173) | error |  | SIGKILL and SIGSTOP cannot be trapped. |
| [SC2174](https://www.shellcheck.net/wiki/SC2174) | warning |  | mkdir -p with -m only sets the mode of the deepest directory. |
| [SC2175](https://www.shellcheck.net/wiki/SC2175) | style |  | A brace range given to eval unquoted is expanded before the variable is substituted. |
| [SC2176](https://www.shellcheck.net/wiki/SC2176) | warning |  | time on a pipeline is undefined in POSIX sh. |
| [SC2177](https://www.shellcheck.net/wiki/SC2177) | warning |  | time on a compound command is undefined in POSIX sh. |
| [SC2178](https://www.shellcheck.net/wiki/SC2178) | warning |  | Assigning a plain string to an array only replaces its first element. |
| [SC2179](https://www.shellcheck.net/wiki/SC2179) | warning |  | Appending a string to an array changes its first element instead of adding one. |
| [SC2181](https://www.shellcheck.net/wiki/SC2181) | style |  | $? is inspected after a command instead of testing the command directly. |
| [SC2182](https://www.shellcheck.net/wiki/SC2182) | error |  | A printf format has no conversion specifiers, so its arguments are ignored. |
| [SC2183](https://www.shellcheck.net/wiki/SC2183) | warning |  | A printf format expects a different number of arguments than it is given. |
| [SC2184](https://www.shellcheck.net/wiki/SC2184) | warning |  | An unquoted argument to unset may have its index brackets expanded as a glob. |
| [SC2185](https://www.shellcheck.net/wiki/SC2185) | info |  | find has no explicit starting path, which some versions require. |
| [SC2186](https://www.shellcheck.net/wiki/SC2186) | warning |  | tempfile is deprecated and missing from many systems. |
| [SC2187](https://www.shellcheck.net/wiki/SC2187) | warning |  | ash has no specification of its own, so the script is checked as dash. |
| [SC2188](https://www.shellcheck.net/wiki/SC2188) | warning |  | A redirection stands alone without a command. |
| [SC2189](https://www.shellcheck.net/wiki/SC2189) | error |  | A pipeline element is only a redirection, with no command to feed. |
| [SC2190](https://www.shellcheck.net/wiki/SC2190) | warning |  | An element of an associative array has no key. |
| [SC2191](https://www.shellcheck.net/wiki/SC2191) | warning |  | An = in an array element is literal text rather than part of \[index\]=value. |
| [SC2192](https://www.shellcheck.net/wiki/SC2192) | warning |  | An array element has an index but no value. |
| [SC2193](https://www.shellcheck.net/wiki/SC2193) | warning |  | No string can match both sides of a comparison, so it never succeeds. |
| [SC2194](https://www.shellcheck.net/wiki/SC2194) | warning |  | The case word is a constant, so the same branch is taken every time. |
| [SC2195](https://www.shellcheck.net/wiki/SC2195) | warning |  | A case pattern can never match the case word. |
| [SC2196](https://www.shellcheck.net/wiki/SC2196) | info |  | egrep is deprecated and missing from some systems. |
| [SC2197](https://www.shellcheck.net/wiki/SC2197) | info |  | fgrep is deprecated and missing from some systems. |
| [SC2198](https://www.shellcheck.net/wiki/SC2198) | warning |  | \[ \] receives each element of an array as a separate word, which breaks the test. |
| [SC2199](https://www.shellcheck.net/wiki/SC2199) | error |  | \[\[ \]\] joins an array expansion into one string. |
| [SC2200](https://www.shellcheck.net/wiki/SC2200) | warning |  | Brace expansion turns a \[ \] operand into several words. |
| [SC2201](https://www.shellcheck.net/wiki/SC2201) | error |  | Brace expansion does not happen inside \[\[ \]\]. |
| [SC2202](https://www.shellcheck.net/wiki/SC2202) | warning |  | A glob in a \[ \] operand can expand to several words. |
| [SC2203](https://www.shellcheck.net/wiki/SC2203) | error |  | \[\[ \]\] does not expand a glob outside the right side of = and !=. |
| [SC2204](https://www.shellcheck.net/wiki/SC2204) | error |  | ( ) runs a subshell, not a test. |
| [SC2205](https://www.shellcheck.net/wiki/SC2205) | warning |  | ( ) runs a subshell, not a comparison. |
| [SC2206](https://www.shellcheck.net/wiki/SC2206) | warning |  | An unquoted expansion is split into words and globbed when building an array. |
| [SC2207](https://www.shellcheck.net/wiki/SC2207) | warning |  | Command output is split on whitespace and globbed when building an array. |
| [SC2208](https://www.shellcheck.net/wiki/SC2208) | error |  | The variable name given to -v is expanded as a glob. |
| [SC2209](https://www.shellcheck.net/wiki/SC2209) | warning |  | A command name is assigned as a string instead of its output. |
| [SC2210](https://www.shellcheck.net/wiki/SC2210) | warning |  | A redirection to a number writes to or reads from a file with that name. |
| [SC2211](https://www.shellcheck.net/wiki/SC2211) | warning |  | A glob is used as a command name. |
| [SC2212](https://www.shellcheck.net/wiki/SC2212) | style |  | An empty test is always false. |
| [SC2213](https://www.shellcheck.net/wiki/SC2213) | warning |  | getopts accepts an option that the case statement has no branch for. |
| [SC2214](https://www.shellcheck.net/wiki/SC2214) | warning |  | A case branch handles an option that is not in the getopts string. |
| [SC2215](https://www.shellcheck.net/wiki/SC2215) | warning |  | A word that looks like an option is used as a command name. |
| [SC2216](https://www.shellcheck.net/wiki/SC2216) | warning |  | A command that does not read standard input is piped data. |
| [SC2217](https://www.shellcheck.net/wiki/SC2217) | warning |  | A command that does not read standard input is given redirected input. |
| [SC2218](https://www.shellcheck.net/wiki/SC2218) | error |  | A function is called before the line that defines it runs. |
| [SC2219](https://www.shellcheck.net/wiki/SC2219) | style |  | let is used where an arithmetic command needs no quoting. |
| [SC2220](https://www.shellcheck.net/wiki/SC2220) | warning |  | A case statement handling getopts options has no \*) branch for invalid options. |
| [SC2221](https://www.shellcheck.net/wiki/SC2221) | warning |  | A case pattern also matches everything a later pattern matches, so that one is never reached. |
| [SC2222](https://www.shellcheck.net/wiki/SC2222) | warning |  | A case pattern never matches because an earlier pattern catches everything it would. |
| [SC2223](https://www.shellcheck.net/wiki/SC2223) | info |  | An unquoted ${var:=default} is globbed on every run. |
| [SC2224](https://www.shellcheck.net/wiki/SC2224) | error |  | mv is given only one path and no destination. |
| [SC2225](https://www.shellcheck.net/wiki/SC2225) | error |  | cp is given only one path and no destination. |
| [SC2226](https://www.shellcheck.net/wiki/SC2226) | warning |  | ln is given only one path and no link name. |
| [SC2227](https://www.shellcheck.net/wiki/SC2227) | warning |  | A redirection on find applies to find itself, not to the command run by -exec. |
| [SC2229](https://www.shellcheck.net/wiki/SC2229) | warning |  | read $name reads into the variable named by the value, not the variable itself. |
| [SC2230](https://www.shellcheck.net/wiki/SC2230) | info | `deprecate-which` | which is not standard and behaves differently between systems. |
| [SC2231](https://www.shellcheck.net/wiki/SC2231) | info |  | An unquoted expansion next to a glob is split and globbed along with it. |
| [SC2232](https://www.shellcheck.net/wiki/SC2232) | warning |  | sudo or doas is given a shell builtin, which has no program to run. |
| [SC2233](https://www.shellcheck.net/wiki/SC2233) | style |  | Parentheses around a condition start a needless subshell. |
| [SC2234](https://www.shellcheck.net/wiki/SC2234) | style |  | Parentheses around a test start a needless subshell. |
| [SC2235](https://www.shellcheck.net/wiki/SC2235) | style |  | Tests are grouped with ( ), which starts a needless subshell. |
| [SC2236](https://www.shellcheck.net/wiki/SC2236) | style | `avoid-negated-conditions` | A negated test operator is written as ! op instead of its opposite. |
| [SC2237](https://www.shellcheck.net/wiki/SC2237) | style | `avoid-negated-conditions` | A negated \[ \] or \[\[ \]\] test is written as ! instead of using the opposite operator. |
| [SC2238](https://www.shellcheck.net/wiki/SC2238) | warning |  | A redirection names a file that is also a command, which suggests a pipe was meant. |
| [SC2239](https://www.shellcheck.net/wiki/SC2239) | error |  | The shebang interpreter is not an absolute path. |
| [SC2240](https://www.shellcheck.net/wiki/SC2240) | warning |  | Arguments after the file given to . are not passed to it in sh. |
| [SC2241](https://www.shellcheck.net/wiki/SC2241) | error |  | exit is given more than one status. |
| [SC2242](https://www.shellcheck.net/wiki/SC2242) | error |  | exit is given a status that is not an integer from 0 to 255. |
| [SC2243](https://www.shellcheck.net/wiki/SC2243) | style | `avoid-nullary-conditions` | A test of a command substitution relies on the implicit non-empty check. |
| [SC2244](https://www.shellcheck.net/wiki/SC2244) | style | `avoid-nullary-conditions` | A test of a string relies on the implicit non-empty check instead of -n. |
| [SC2245](https://www.shellcheck.net/wiki/SC2245) | warning |  | ksh applies a file test operator only to the first match of a glob. |
| [SC2246](https://www.shellcheck.net/wiki/SC2246) | error |  | The shebang interpreter ends in a slash, so it names a directory. |
| [SC2247](https://www.shellcheck.net/wiki/SC2247) | warning |  | $" starts a translated string where a quoted substitution was meant. |
| [SC2248](https://www.shellcheck.net/wiki/SC2248) | style | `quote-safe-variables` | A variable that holds no spaces or globs today is expanded unquoted. |
| [SC2249](https://www.shellcheck.net/wiki/SC2249) | info | `add-default-case` | A case statement has no \*) branch for unexpected values. |
| [SC2250](https://www.shellcheck.net/wiki/SC2250) | style | `require-variable-braces` | A variable is expanded without braces. |
| [SC2251](https://www.shellcheck.net/wiki/SC2251) | info |  | A negated command never triggers set -e. |
| [SC2252](https://www.shellcheck.net/wiki/SC2252) | warning |  | A value always differs from at least one of several constants, so a \|\| chain of != tests is always true. |
| [SC2253](https://www.shellcheck.net/wiki/SC2253) | warning |  | chmod -r removes read permission instead of changing modes recursively. |
| [SC2254](https://www.shellcheck.net/wiki/SC2254) | warning |  | An unquoted expansion in a case pattern is matched as a glob. |
| [SC2255](https://www.shellcheck.net/wiki/SC2255) | error |  | A numeric operand in \[ \] is an unquoted glob, not a number. |
| [SC2256](https://www.shellcheck.net/wiki/SC2256) | warning |  | $"name" is a translated string, not an expansion of the variable. |
| [SC2257](https://www.shellcheck.net/wiki/SC2257) | warning |  | Arithmetic that modifies a variable in a redirection may run in a subshell or not at all. |
| [SC2258](https://www.shellcheck.net/wiki/SC2258) | warning |  | A trailing comma becomes part of a loop item. |
| [SC2259](https://www.shellcheck.net/wiki/SC2259) | error |  | An input redirection replaces the piped input, discarding the previous command's output. |
| [SC2260](https://www.shellcheck.net/wiki/SC2260) | error |  | An output redirection takes the output away from the pipe, so the next command reads nothing. |
| [SC2261](https://www.shellcheck.net/wiki/SC2261) | error |  | Several redirections compete for the same file descriptor, and only the last takes effect. |
| [SC2262](https://www.shellcheck.net/wiki/SC2262) | warning |  | An alias is used in the same parsing unit that defines it, where it does not take effect yet. |
| [SC2263](https://www.shellcheck.net/wiki/SC2263) | info |  | A command runs as a program instead of the alias defined in the same parsing unit. |
| [SC2264](https://www.shellcheck.net/wiki/SC2264) | error |  | A function that wraps a command calls itself unconditionally and recurses forever. |
| [SC2265](https://www.shellcheck.net/wiki/SC2265) | error |  | A single & between tests runs the first in the background instead of combining them. |
| [SC2266](https://www.shellcheck.net/wiki/SC2266) | warning |  | A single \| between tests pipes between them instead of combining them. |
| [SC2267](https://www.shellcheck.net/wiki/SC2267) | info |  | xargs -i is deprecated. |
| [SC2268](https://www.shellcheck.net/wiki/SC2268) | style |  | The x prefix in comparisons is an obsolete workaround. |
| [SC2269](https://www.shellcheck.net/wiki/SC2269) | info |  | A variable is assigned its own value, which does nothing. |
| [SC2270](https://www.shellcheck.net/wiki/SC2270) | error |  | Positional parameters cannot be assigned with =. |
| [SC2271](https://www.shellcheck.net/wiki/SC2271) | error |  | A variable name computed from an expansion cannot be assigned with =. |
| [SC2272](https://www.shellcheck.net/wiki/SC2272) | error |  | A command name contains ==, which suggests a comparison or assignment was meant. |
| [SC2273](https://www.shellcheck.net/wiki/SC2273) | error |  | A line of equals signs looks like a leftover merge conflict marker or text border. |
| [SC2274](https://www.shellcheck.net/wiki/SC2274) | error |  | A command cannot start with ===. |
| [SC2275](https://www.shellcheck.net/wiki/SC2275) | error |  | A command cannot start with =. |
| [SC2276](https://www.shellcheck.net/wiki/SC2276) | error |  | A word with = is run as a command because the part before = is not a valid variable name. |
| [SC2277](https://www.shellcheck.net/wiki/SC2277) | error |  | $0 cannot be assigned like a variable in bash. |
| [SC2278](https://www.shellcheck.net/wiki/SC2278) | error |  | $0 cannot be assigned in ksh. |
| [SC2279](https://www.shellcheck.net/wiki/SC2279) | error |  | $0 cannot be assigned in this shell. |
| [SC2280](https://www.shellcheck.net/wiki/SC2280) | error |  | $0 cannot be assigned in sh. |
| [SC2281](https://www.shellcheck.net/wiki/SC2281) | error |  | The left side of an assignment has a $. |
| [SC2282](https://www.shellcheck.net/wiki/SC2282) | error |  | Variable names cannot start with a digit, so the assignment runs as a command. |
| [SC2283](https://www.shellcheck.net/wiki/SC2283) | error |  | Spaces around = turn an assignment into a command. |
| [SC2284](https://www.shellcheck.net/wiki/SC2284) | error |  | == on its own is neither an assignment nor a command. |
| [SC2285](https://www.shellcheck.net/wiki/SC2285) | error |  | Spaces around += turn an append into a command. |
| [SC2286](https://www.shellcheck.net/wiki/SC2286) | error |  | The command name is an empty string. |
| [SC2287](https://www.shellcheck.net/wiki/SC2287) | error |  | A command name ends in a slash, so it names a directory. |
| [SC2288](https://www.shellcheck.net/wiki/SC2288) | warning |  | A command name ends in a character that suggests a stray character or a missing space. |
| [SC2289](https://www.shellcheck.net/wiki/SC2289) | error |  | A command name contains a tab or a line break. |
| [SC2290](https://www.shellcheck.net/wiki/SC2290) | error |  | A space before = or += in a declaration turns the assignment into a separate word. |
| [SC2291](https://www.shellcheck.net/wiki/SC2291) | info |  | Unquoted runs of spaces between echo arguments collapse to a single space. |
| [SC2292](https://www.shellcheck.net/wiki/SC2292) | style | `require-double-brackets` | \[ \] is used for a test where \[\[ \]\] is available. |
| [SC2293](https://www.shellcheck.net/wiki/SC2293) | style |  | eval is given ${array\[@\]@Q}, although it joins its arguments with spaces anyway. |
| [SC2294](https://www.shellcheck.net/wiki/SC2294) | warning |  | eval joins array elements into one string and re-splits it, losing their boundaries. |
| [SC2295](https://www.shellcheck.net/wiki/SC2295) | info |  | An unquoted expansion inside a pattern is matched as a glob. |
| [SC2296](https://www.shellcheck.net/wiki/SC2296) | error |  | A parameter expansion starts with a character no parameter name can start with. |
| [SC2297](https://www.shellcheck.net/wiki/SC2297) | error |  | Double quotes are used inside ${ }. |
| [SC2298](https://www.shellcheck.net/wiki/SC2298) | error |  | An expansion is used as the name in ${ }. |
| [SC2299](https://www.shellcheck.net/wiki/SC2299) | error |  | Parameter expansions cannot be nested. |
| [SC2300](https://www.shellcheck.net/wiki/SC2300) | error |  | Parameter expansion cannot operate on a command substitution. |
| [SC2301](https://www.shellcheck.net/wiki/SC2301) | error |  | A parameter expansion starts with quotes or other unexpected syntax. |
| [SC2302](https://www.shellcheck.net/wiki/SC2302) | warning |  | A loop iterates over an associative array's values but uses the loop variable as a key. |
| [SC2303](https://www.shellcheck.net/wiki/SC2303) | warning |  | A value taken from an associative array is used as a key. |
| [SC2304](https://www.shellcheck.net/wiki/SC2304) | error |  | An unescaped \* given to expr is expanded as a glob. |
| [SC2305](https://www.shellcheck.net/wiki/SC2305) | warning |  | An unquoted expr regex may be expanded as a glob. |
| [SC2306](https://www.shellcheck.net/wiki/SC2306) | warning |  | Unescaped glob characters in expr arguments may be expanded by the shell. |
| [SC2307](https://www.shellcheck.net/wiki/SC2307) | warning |  | expr is given too few arguments to form an expression. |
| [SC2308](https://www.shellcheck.net/wiki/SC2308) | info |  | An expr keyword is not specified by POSIX. |
| [SC2309](https://www.shellcheck.net/wiki/SC2309) | warning |  | A numeric operator inside \[\[ \]\] evaluates its operand as arithmetic. |
| [SC2310](https://www.shellcheck.net/wiki/SC2310) | info | `check-set-e-suppressed` | set -e has no effect inside a function invoked as a condition. |
| [SC2311](https://www.shellcheck.net/wiki/SC2311) | info | `check-set-e-suppressed` | set -e has no effect inside a command substitution that does not inherit it. |
| [SC2312](https://www.shellcheck.net/wiki/SC2312) | info | `check-extra-masked-returns` | A command's exit status is masked by its context. |
| [SC2313](https://www.shellcheck.net/wiki/SC2313) | warning |  | An unquoted array index in a read target may be expanded as a glob. |
| [SC2314](https://www.shellcheck.net/wiki/SC2314) | error |  | Bats ignores the status of a negated command, so the line cannot fail the test. |
| [SC2315](https://www.shellcheck.net/wiki/SC2315) | error |  | Bats ignores the status of a negated test, so the line cannot fail the test. |
| [SC2316](https://www.shellcheck.net/wiki/SC2316) | error |  | A word given to a declaration command is parsed as a variable name, not as a modifier. |
| [SC2317](https://www.shellcheck.net/wiki/SC2317) | info |  | A command can never run, because every path to it exits, returns or loops forever first. |
| [SC2318](https://www.shellcheck.net/wiki/SC2318) | warning |  | A variable assigned in a declaration is read again in the same declaration before it takes effect. |
| [SC2319](https://www.shellcheck.net/wiki/SC2319) | warning |  | $? after a test is the status of that test, not of the command it checked. |
| [SC2320](https://www.shellcheck.net/wiki/SC2320) | warning |  | $? after echo is the status of echo, not of the command before it. |
| [SC2321](https://www.shellcheck.net/wiki/SC2321) | style |  | $(( )) around an array index is redundant, since indices are already arithmetic. |
| [SC2322](https://www.shellcheck.net/wiki/SC2322) | style |  | Doubled parentheses in arithmetic are redundant. |
| [SC2323](https://www.shellcheck.net/wiki/SC2323) | style |  | Parentheses around a whole arithmetic expression are redundant. |
| [SC2324](https://www.shellcheck.net/wiki/SC2324) | warning |  | += on a plain variable appends text instead of adding a number. |
| [SC2325](https://www.shellcheck.net/wiki/SC2325) | error |  | sh allows only one ! before a pipeline. |
| [SC2326](https://www.shellcheck.net/wiki/SC2326) | error |  | ! cannot negate a single stage after a \|. |
| [SC2327](https://www.shellcheck.net/wiki/SC2327) | warning |  | A command substitution is always empty because the command's output is redirected elsewhere. |
| [SC2328](https://www.shellcheck.net/wiki/SC2328) | error |  | A redirection takes the output away from the command substitution it is in. |
| [SC2329](https://www.shellcheck.net/wiki/SC2329) | info |  | A function is never invoked, and the script always exits before it could be sourced and called. |
| [SC2331](https://www.shellcheck.net/wiki/SC2331) | style |  | -a as a file test is a legacy synonym of -e that is easily confused with the -a conjunction. |
| [SC2332](https://www.shellcheck.net/wiki/SC2332) | error |  | \[ ! -o name \] or \[ ! -a file \] reads -o or -a as a binary operator and is always true. |
| [SC2333](https://www.shellcheck.net/wiki/SC2333) | warning |  | A value cannot equal two different constants at once, so the condition is always false. |
| [SC2334](https://www.shellcheck.net/wiki/SC2334) | warning |  | A value cannot equal two different numbers at once, so the && is always false. |
| [SC2335](https://www.shellcheck.net/wiki/SC2335) | style | `avoid-negated-conditions` | A negated comparison is written as ! a op b instead of using the inverted operator. |
| [SC2336](https://www.shellcheck.net/wiki/SC2336) | warning |  | cp -r is implementation-defined for special files. |
| [SC2337](https://www.shellcheck.net/wiki/SC2337) | warning |  | A command that may exit before reading all its input can make the writer upstream fail with SIGPIPE under pipefail. |
| [SC2338](https://www.shellcheck.net/wiki/SC2338) | style | `require-double-equals` | = is used for string comparison in a bash test where == is preferred. |
| [SC3001](https://www.shellcheck.net/wiki/SC3001) | warning |  | Process substitution is undefined in POSIX sh. |
| [SC3002](https://www.shellcheck.net/wiki/SC3002) | warning |  | Extended globs are undefined in POSIX sh. |
| [SC3004](https://www.shellcheck.net/wiki/SC3004) | warning |  | Locale-translated $"..." strings are undefined in POSIX sh. |
| [SC3005](https://www.shellcheck.net/wiki/SC3005) | warning |  | C-style for ((...)) loops are undefined in POSIX sh. |
| [SC3006](https://www.shellcheck.net/wiki/SC3006) | warning |  | The arithmetic command ((...)) is undefined in POSIX sh. |
| [SC3007](https://www.shellcheck.net/wiki/SC3007) | warning |  | $\[...\] arithmetic is undefined in POSIX sh. |
| [SC3008](https://www.shellcheck.net/wiki/SC3008) | warning |  | select loops are undefined in POSIX sh. |
| [SC3009](https://www.shellcheck.net/wiki/SC3009) | warning |  | Brace expansion is undefined in POSIX sh. |
| [SC3010](https://www.shellcheck.net/wiki/SC3010) | warning |  | \[\[ \]\] tests are undefined in POSIX sh and dash. |
| [SC3011](https://www.shellcheck.net/wiki/SC3011) | warning |  | Here-strings with \<\<\< are undefined in POSIX sh. |
| [SC3012](https://www.shellcheck.net/wiki/SC3012) | warning |  | Lexicographic comparison with \< or \> in \[ \] is undefined in POSIX sh. |
| [SC3014](https://www.shellcheck.net/wiki/SC3014) | warning |  | == in \[ \] is undefined in POSIX sh and dash. |
| [SC3015](https://www.shellcheck.net/wiki/SC3015) | warning |  | Regex matching with =~ is undefined in POSIX sh. |
| [SC3016](https://www.shellcheck.net/wiki/SC3016) | warning |  | test -v is undefined in POSIX sh. |
| [SC3017](https://www.shellcheck.net/wiki/SC3017) | warning |  | The unary test -a is undefined in POSIX sh. |
| [SC3018](https://www.shellcheck.net/wiki/SC3018) | warning |  | The ++ and -- operators are undefined in POSIX sh. |
| [SC3019](https://www.shellcheck.net/wiki/SC3019) | warning |  | Exponentiation with \*\* is undefined in POSIX sh. |
| [SC3020](https://www.shellcheck.net/wiki/SC3020) | warning |  | Redirecting both streams with &\> is undefined in POSIX sh and dash. |
| [SC3021](https://www.shellcheck.net/wiki/SC3021) | warning |  | Redirecting both streams to a file with \>& is undefined in POSIX sh. |
| [SC3022](https://www.shellcheck.net/wiki/SC3022) | warning |  | Named file descriptors like {fd}\> are undefined in POSIX sh. |
| [SC3023](https://www.shellcheck.net/wiki/SC3023) | warning |  | File descriptor numbers above 9 are undefined in POSIX sh. |
| [SC3024](https://www.shellcheck.net/wiki/SC3024) | warning |  | Appending with += is undefined in POSIX sh. |
| [SC3025](https://www.shellcheck.net/wiki/SC3025) | warning |  | Network pseudo-devices such as /dev/tcp are undefined in POSIX sh. |
| [SC3026](https://www.shellcheck.net/wiki/SC3026) | warning |  | Negating a bracket expression with ^ is undefined in POSIX sh. |
| [SC3028](https://www.shellcheck.net/wiki/SC3028) | warning |  | A bash-specific variable is undefined in POSIX sh. |
| [SC3029](https://www.shellcheck.net/wiki/SC3029) | warning |  | The \|& operator is undefined in POSIX sh. |
| [SC3030](https://www.shellcheck.net/wiki/SC3030) | warning |  | Arrays are undefined in POSIX sh. |
| [SC3031](https://www.shellcheck.net/wiki/SC3031) | warning |  | Redirecting to or from a glob is undefined in POSIX sh. |
| [SC3032](https://www.shellcheck.net/wiki/SC3032) | warning |  | coproc is undefined in POSIX sh. |
| [SC3033](https://www.shellcheck.net/wiki/SC3033) | warning |  | A function name that is not a valid identifier is undefined in POSIX sh. |
| [SC3034](https://www.shellcheck.net/wiki/SC3034) | warning |  | Reading a file with $(\< file) is undefined in POSIX sh. |
| [SC3035](https://www.shellcheck.net/wiki/SC3035) | warning |  | Reading a file with \`\< file\` is undefined in POSIX sh. |
| [SC3036](https://www.shellcheck.net/wiki/SC3036) | error |  | echo in dash and busybox sh does not support this flag. |
| [SC3037](https://www.shellcheck.net/wiki/SC3037) | warning |  | echo flags are undefined in POSIX sh. |
| [SC3038](https://www.shellcheck.net/wiki/SC3038) | warning |  | exec options are undefined in POSIX sh. |
| [SC3039](https://www.shellcheck.net/wiki/SC3039) | warning |  | let is undefined in POSIX sh. |
| [SC3040](https://www.shellcheck.net/wiki/SC3040) | warning |  | This set -o option is undefined in POSIX sh. |
| [SC3041](https://www.shellcheck.net/wiki/SC3041) | warning |  | This set flag is undefined in POSIX sh. |
| [SC3042](https://www.shellcheck.net/wiki/SC3042) | warning |  | This set option is undefined in POSIX sh. |
| [SC3043](https://www.shellcheck.net/wiki/SC3043) | warning |  | local is undefined in POSIX sh. |
| [SC3044](https://www.shellcheck.net/wiki/SC3044) | warning |  | A bash builtin is used that is undefined in POSIX sh. |
| [SC3045](https://www.shellcheck.net/wiki/SC3045) | warning |  | A builtin option is used that is undefined in POSIX sh. |
| [SC3046](https://www.shellcheck.net/wiki/SC3046) | warning |  | source is undefined in POSIX sh and dash. |
| [SC3047](https://www.shellcheck.net/wiki/SC3047) | warning |  | Trapping this signal or condition is undefined in POSIX sh. |
| [SC3048](https://www.shellcheck.net/wiki/SC3048) | warning |  | The SIG prefix on a signal name is undefined in POSIX sh and dash. |
| [SC3049](https://www.shellcheck.net/wiki/SC3049) | warning |  | Lowercase signal names are undefined in POSIX sh. |
| [SC3050](https://www.shellcheck.net/wiki/SC3050) | warning |  | printf %q is undefined in POSIX sh. |
| [SC3051](https://www.shellcheck.net/wiki/SC3051) | warning |  | source is undefined in POSIX sh and dash, also when the file it loads is followed. |
| [SC3052](https://www.shellcheck.net/wiki/SC3052) | warning |  | Arithmetic base prefixes like 16# are undefined in POSIX sh. |
| [SC3053](https://www.shellcheck.net/wiki/SC3053) | warning |  | Indirect expansion with ${!name} is undefined in POSIX sh. |
| [SC3054](https://www.shellcheck.net/wiki/SC3054) | warning |  | Array subscripts are undefined in POSIX sh. |
| [SC3055](https://www.shellcheck.net/wiki/SC3055) | warning |  | Listing array keys with ${!array\[@\]} is undefined in POSIX sh. |
| [SC3056](https://www.shellcheck.net/wiki/SC3056) | warning |  | Listing variable names by prefix with ${!prefix\*} is undefined in POSIX sh. |
| [SC3057](https://www.shellcheck.net/wiki/SC3057) | warning |  | Substring expansion ${var:offset} is undefined in POSIX sh and dash. |
| [SC3058](https://www.shellcheck.net/wiki/SC3058) | warning |  | Applying a length or trim operator to $@ or $\* is undefined in POSIX sh and dash. |
| [SC3059](https://www.shellcheck.net/wiki/SC3059) | warning |  | Case modification in parameter expansion is undefined in POSIX sh. |
| [SC3060](https://www.shellcheck.net/wiki/SC3060) | warning |  | Pattern substitution ${var/pattern/replacement} is undefined in POSIX sh and dash. |
| [SC3061](https://www.shellcheck.net/wiki/SC3061) | warning |  | read without a variable name is undefined in POSIX sh. |
| [SC3062](https://www.shellcheck.net/wiki/SC3062) | warning |  | Checking a shell option with test -o is undefined in POSIX sh. |
| [SC3063](https://www.shellcheck.net/wiki/SC3063) | warning |  | test -R is undefined in POSIX sh. |
| [SC3064](https://www.shellcheck.net/wiki/SC3064) | warning |  | test -N is undefined in POSIX sh. |
| [SC3065](https://www.shellcheck.net/wiki/SC3065) | warning |  | test -k is undefined in POSIX sh. |
| [SC3066](https://www.shellcheck.net/wiki/SC3066) | warning |  | test -G is undefined in POSIX sh. |
| [SC3067](https://www.shellcheck.net/wiki/SC3067) | warning |  | test -O is undefined in POSIX sh. |
| [SL1001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1001) | warning |  | eval runs untrusted data as code. |
| [SL1002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1002) | warning |  | A shell -c script is built from untrusted data. |
| [SL1003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1003) | warning | `arithmetic-injection` | Untrusted data is evaluated as arithmetic. |
| [SL1004](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1004) | warning |  | A sourced path is built from untrusted data. |
| [SL1005](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl1005) | warning |  | Downloaded code runs unchecked. |
| [SL2001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2001) | style |  | A function in a script is never called. |
| [SL2002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2002) | warning |  | An \`@\` transformation the shell does not know. |
| [SL2003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2003) | error |  | A declaration assigns a name that is not a variable name. |
| [SL2004](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2004) | warning |  | A trap is set inside a subshell. |
| [SL2005](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2005) | warning |  | exit only leaves a subshell and nobody checks its status. |
| [SL2006](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2006) | info |  | A temporary file or directory is never removed. |
| [SL2007](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2007) | info |  | A function runs pushd but never popd. |
| [SL2008](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2008) | error |  | A \`${…}\` the shell rejects when it runs it. |
| [SL2009](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl2009) | info | `unused-directives` | A disabled check that nothing here reports. |
| [SL3001](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3001) | error |  | An awk program does not parse. |
| [SL3002](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3002) | error |  | A sed script does not parse. |
| [SL3003](https://github.com/stubbedev/shell-lsp/blob/main/docs/rules.md#sl3003) | error |  | A jq filter does not parse. |
