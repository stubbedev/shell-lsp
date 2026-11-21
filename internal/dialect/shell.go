package dialect

import (
	"fmt"
	"iter"
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
	features Features
	detect   func(Input) bool
}

var flavors = [...]flavorSpec{
	FlavorBats: {of: Bash, language: syntax.LangBats, features: specs[Bash].features, detect: Input.bats},
	FlavorMksh: {
		of: Ksh, language: syntax.LangMirBSDKorn,
		features: Features{
			Arrays: true, DoubleBracket: true, Local: true, SplitsUnquoted: true, GlobsUnquoted: true,
			DeclarationAssigns: true, QuotedRegexLiteral: true, SubscriptsRunCode: true,
		},
		detect: Input.mksh,
	},
}

type Shell struct {
	Dialect Dialect
	Flavor  Flavor
}

func (d Dialect) Shell() Shell { return Shell{Dialect: d} }

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
	return d.Shell()
}

func (s Shell) Lang() syntax.LangVariant {
	if s.Flavor == FlavorNone {
		return s.Dialect.Language()
	}
	return s.flavor().language
}

func (s Shell) flavor() flavorSpec {
	if int(s.Flavor) >= len(flavors) || flavors[s.Flavor].of != s.Dialect {
		panic(fmt.Sprintf("dialect: flavor %d is not a flavor of %s", s.Flavor, s.Dialect))
	}
	return flavors[s.Flavor]
}

func (s Shell) Features() Features {
	if s.Flavor == FlavorNone {
		return s.Dialect.spec().features
	}
	return s.flavor().features
}

func (s Shell) Bats() bool { return s.Flavor == FlavorBats }

type Shells uint16

func (s Shell) bit() Shells {
	if s.Flavor == FlavorNone {
		return 1 << s.Dialect
	}
	_ = s.flavor()
	return 1 << (len(specs) + int(s.Flavor) - 1)
}

func ShellsOf(shells ...Shell) Shells {
	var out Shells
	for _, s := range shells {
		out |= s.bit()
	}
	return out
}

func (s Set) Shells() Shells {
	var out Shells
	for d := range s.Members() {
		out |= d.Shell().bit()
	}
	for f, spec := range flavors {
		if Flavor(f) != FlavorNone && s.Has(spec.of) {
			out |= Flavor(f).Shell().bit()
		}
	}
	return out
}

func (s Shells) Has(sh Shell) bool { return s&sh.bit() != 0 }

func (s Shells) Members() iter.Seq[Shell] {
	return func(yield func(Shell) bool) {
		for d := range All.Members() {
			if s.Has(d.Shell()) && !yield(d.Shell()) {
				return
			}
		}
		for f := range flavors {
			if Flavor(f) != FlavorNone && s.Has(Flavor(f).Shell()) && !yield(Flavor(f).Shell()) {
				return
			}
		}
	}
}

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
