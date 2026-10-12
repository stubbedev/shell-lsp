package lint

import (
	"iter"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/parser"
	"github.com/stubbedev/shell-lsp/third_party/mvdan-sh/syntax"
)

const codeWorldWritable = nativeBase + 1006

var permissionsFamily = family{
	codes: map[Code]meta{
		codeWorldWritable: {severity: SeverityWarning},
	},
	nodes: []nodeRule{onNode(checkWorldWritable)},
}

const unknownPathPart = "\x00"

var (
	systemTrees    = setOf("bin", "boot", "etc", "lib", "lib32", "lib64", "root", "sbin", "usr")
	systemDirs     = setOf("home", "var")
	secretDirs     = setOf(".ssh", ".gnupg")
	privateKeys    = setOf("id_rsa", "id_dsa", "id_ecdsa", "id_ecdsa_sk", "id_ed25519", "id_ed25519_sk")
	authorizedKeys = setOf("authorized_keys", "authorized_keys2")
)

func checkWorldWritable(p *pass, call *syntax.CallExpr) {
	command, ok := p.runs(call.Args)
	if !ok {
		return
	}
	mode, targets, ok := modeChange(path.Base(command.name), command.args)
	if !ok || !grantsOthersWrite(mode) {
		return
	}
	for _, target := range targets {
		span, ok := p.span(target)
		if !ok {
			continue
		}
		if kind, ok := sensitivePath(pathShape(target)); ok {
			p.report(codeWorldWritable, span, "Mode "+mode+" lets every user on the system write to "+p.excerpt(span)+", "+kind+"; take write permission away from others, as in 755 or 644.")
		}
	}
}

func modeChange(name string, args []*syntax.Word) (string, []*syntax.Word, bool) {
	parsed := parseOptions(name, args)
	if !parsed.Complete {
		return "", nil, false
	}
	switch name {
	case "chmod":
		if parsed.Has("--reference") || len(parsed.Operands) < 2 {
			return "", nil, false
		}
		mode, ok := parser.Literal(parsed.Operands[0])
		return mode, parsed.Operands[1:], ok
	case "mkdir":
		mode, ok := modeOption(parsed)
		return mode, parsed.Operands, ok
	case "install":
		mode, ok := modeOption(parsed)
		return mode, installedPaths(parsed), ok
	}
	return "", nil, false
}

func modeOption(parsed options) (string, bool) {
	for _, opt := range slices.Backward(parsed.Options) {
		if opt.Name == "-m" || opt.Name == "--mode" {
			return opt.Argument.Text(parser.Literal)
		}
	}
	return "", false
}

func installedPaths(parsed options) []*syntax.Word {
	for _, opt := range parsed.Options {
		if (opt.Name == "-t" || opt.Name == "--target-directory") && opt.Argument != nil && opt.Argument.Offset == 0 {
			return []*syntax.Word{opt.Argument.Word}
		}
	}
	if parsed.Has("-d", "--directory") || len(parsed.Operands) == 0 {
		return parsed.Operands
	}
	return parsed.Operands[len(parsed.Operands)-1:]
}

func grantsOthersWrite(mode string) bool {
	if mode != "" && strings.Trim(mode, "01234567") == "" {
		return (mode[len(mode)-1]-'0')&2 != 0
	}
	granted := false
	for clause := range strings.SplitSeq(mode, ",") {
		who := strings.TrimLeft(clause, "ugoa")
		if !strings.ContainsAny(clause[:len(clause)-len(who)], "oa") {
			continue
		}
		for action := range symbolicActions(who) {
			switch action.op {
			case '+':
				granted = granted || strings.ContainsRune(action.perms, 'w')
			case '-':
				granted = granted && !strings.ContainsRune(action.perms, 'w')
			case '=':
				granted = strings.ContainsRune(action.perms, 'w')
			}
		}
	}
	return granted
}

type symbolicAction struct {
	op    byte
	perms string
}

func symbolicActions(actions string) iter.Seq[symbolicAction] {
	return func(yield func(symbolicAction) bool) {
		for actions != "" && strings.IndexByte("+-=", actions[0]) >= 0 {
			end := 1 + strings.IndexAny(actions[1:]+"+", "+-=")
			if !yield(symbolicAction{op: actions[0], perms: actions[1:end]}) {
				return
			}
			actions = actions[end:]
		}
	}
}

func pathShape(w *syntax.Word) string {
	var sb strings.Builder
	for i, part := range w.Parts {
		switch part := part.(type) {
		case *syntax.Lit:
			sb.WriteString(part.Value)
		case *syntax.SglQuoted:
			if part.Dollar {
				sb.WriteString(unknownPathPart)
				continue
			}
			sb.WriteString(part.Value)
		case *syntax.DblQuoted:
			for j, inner := range part.Parts {
				sb.WriteString(shapeOf(inner, i == 0 && j == 0))
			}
		default:
			sb.WriteString(shapeOf(part, i == 0))
		}
	}
	return sb.String()
}

func shapeOf(part syntax.WordPart, first bool) string {
	switch part := part.(type) {
	case *syntax.Lit:
		return part.Value
	case *syntax.ParamExp:
		if first && parser.IsPlainParam(part) && part.Param.Value == "HOME" {
			return "~"
		}
	}
	return unknownPathPart
}

func sensitivePath(shape string) (string, bool) {
	components := slices.DeleteFunc(strings.Split(shape, "/"), func(c string) bool { return c == "" || c == "." })
	switch {
	case strings.HasPrefix(shape, "~"):
		if len(components) == 1 {
			return "a home directory", true
		}
		if len(components) == 2 && dialect.IsRCFile(components[1]) {
			return "a shell startup file", true
		}
	case strings.HasPrefix(shape, "/"):
		switch {
		case len(components) == 0:
			return "the root directory", true
		case systemTrees[components[0]]:
			return "a system directory", true
		case len(components) == 1 && systemDirs[components[0]]:
			return "a system directory", true
		case len(components) == 2 && components[0] == "home":
			return "a home directory", true
		}
	}
	if len(components) == 0 {
		return "", false
	}
	switch base := components[len(components)-1]; {
	case authorizedKeys[base]:
		return "the list of keys that may log in over SSH", true
	case privateKeys[base]:
		return "an SSH private key", true
	case secretDirs[base]:
		return "a directory of private keys", true
	case slices.ContainsFunc(components, func(c string) bool { return secretDirs[c] }):
		return "part of a directory of private keys", true
	}
	return "", false
}
