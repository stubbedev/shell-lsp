package dialect

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

type Flavor uint8

const (
	FlavorNone Flavor = iota
	FlavorBats
	FlavorMksh
)

type flavorSpec struct {
	of       Dialect
	language syntax.LangVariant
	detect   func(Input) bool
}

var flavors = [...]flavorSpec{
	FlavorBats: {of: Bash, language: syntax.LangBats, detect: Input.bats},
	FlavorMksh: {of: Ksh, language: syntax.LangMirBSDKorn, detect: Input.mksh},
}

type Shell struct {
	Dialect Dialect
	Flavor  Flavor
}

func (f Flavor) Shell() Shell {
	if f == FlavorNone || int(f) >= len(flavors) {
		panic(fmt.Sprintf("dialect: flavor %d names no shell", f))
	}
	return Shell{Dialect: flavors[f].of, Flavor: f}
}

func ShellOf(d Dialect, in Input) Shell {
	for f, spec := range flavors {
		if Flavor(f) != FlavorNone && spec.of == d && spec.detect(in) {
			return Shell{Dialect: d, Flavor: Flavor(f)}
		}
	}
	return Shell{Dialect: d}
}

func (s Shell) Lang() syntax.LangVariant {
	if s.Flavor == FlavorNone {
		return s.Dialect.Language()
	}
	if int(s.Flavor) >= len(flavors) || flavors[s.Flavor].of != s.Dialect {
		panic(fmt.Sprintf("dialect: flavor %d is not a flavor of %s", s.Flavor, s.Dialect))
	}
	return flavors[s.Flavor].language
}

func (s Shell) Bats() bool { return s.Flavor == FlavorBats }

func (in Input) bats() bool {
	if strings.HasSuffix(in.URI, ".bats") {
		return true
	}
	name, ok := Interpreter(in.Text)
	return ok && name == "bats"
}

func (in Input) mksh() bool {
	if name, ok := headerShellDirective(in.Text); ok {
		return name == "mksh"
	}
	if name, ok := Interpreter(in.Text); ok {
		return name == "mksh"
	}
	return filepath.Base(in.URI) == ".mkshrc"
}
