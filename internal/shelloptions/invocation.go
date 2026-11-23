package shelloptions

import "strings"

func SetCommand(flags []string) string {
	var options []string
	for i := 0; i < len(flags); i++ {
		word := flags[i]
		if word == "--" || !strings.HasPrefix(word, "-") {
			break
		}
		if strings.HasPrefix(word, "--") {
			continue
		}
		letters, owes := strings.CutSuffix(strings.ReplaceAll(word[1:], "c", ""), "o")
		named := owes && i+1 < len(flags)
		if letters != "" {
			options = append(options, "-"+letters)
		}
		if named {
			options = append(options, "-o", flags[i+1])
			i++
		}
	}
	if len(options) == 0 {
		return ""
	}
	return "set " + strings.Join(options, " ") + "\n"
}
