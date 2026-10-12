package dialect

import "fmt"

type Source uint8

const (
	SourceDefault Source = iota
	SourceOverride
	SourceEditorConfig
	SourceDirective
	SourceShebang
	SourceCompletionTag
	SourceFileName
	SourceExtension
	SourceLanguageID
	SourceModeline
	SourceHostSetting
	SourceHostDefault
)

const sourceCount = int(SourceHostDefault) + 1

var sourceNames = [sourceCount]string{
	SourceDefault:       "default",
	SourceOverride:      "override",
	SourceEditorConfig:  "editorconfig",
	SourceDirective:     "directive",
	SourceShebang:       "shebang",
	SourceCompletionTag: "completionTag",
	SourceFileName:      "fileName",
	SourceExtension:     "extension",
	SourceLanguageID:    "languageId",
	SourceModeline:      "modeline",
	SourceHostSetting:   "hostSetting",
	SourceHostDefault:   "hostDefault",
}

func (s Source) String() string {
	if int(s) >= sourceCount || sourceNames[s] == "" {
		panic(fmt.Sprintf("dialect: unknown detection source %d", s))
	}
	return sourceNames[s]
}

type Detection struct {
	Shell
	Source   Source
	Evidence string
}

func (d Detection) Named() bool { return d.Source != SourceDefault }

func (d Detection) Guessed() bool {
	return d.Source == SourceDefault || d.Source == SourceExtension
}

func (d Detection) Describe() string {
	switch d.Source {
	case SourceOverride:
		return "set by the dialect setting"
	case SourceEditorConfig:
		return "from shell_variant = " + d.Evidence + " in .editorconfig"
	case SourceDirective:
		if _, known := FromName(d.Evidence); !known {
			return "from the directive # shellcheck shell=" + d.Evidence + "; " + d.Evidence + " is not a shell shell-lsp knows, so it is checked as " + d.Dialect.String()
		}
		return "from the directive # shellcheck shell=" + d.Evidence
	case SourceShebang:
		return "from the shebang " + d.Evidence
	case SourceCompletionTag:
		return "from the zsh completion tag " + d.Evidence
	case SourceFileName:
		return "from the file name " + d.Evidence
	case SourceExtension:
		return "guessed from the " + d.Evidence + " extension"
	case SourceLanguageID:
		return "from the editor language " + d.Evidence
	case SourceModeline:
		return "from the modeline ft=" + d.Evidence
	case SourceHostSetting:
		return "from " + d.Evidence
	case SourceHostDefault:
		return "the shell " + d.Evidence + " runs scripts with unless told otherwise"
	case SourceDefault:
	}
	return "guessed: nothing names the shell, so " + d.Dialect.String() + " is assumed"
}
