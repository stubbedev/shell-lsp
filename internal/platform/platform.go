package platform

import (
	"io/fs"
	"path"
	"slices"
	"strings"
)

type Flavour uint8

const (
	POSIX Flavour = iota
	Windows
)

func (f Flavour) String() string {
	if f == Windows {
		return "windows"
	}
	return "posix"
}

func (f Flavour) Separator() byte {
	if f == Windows {
		return '\\'
	}
	return '/'
}

func (f Flavour) Clean(p string) string {
	if f == POSIX {
		return path.Clean(p)
	}
	volume, rest := windowsVolume(strings.ReplaceAll(p, `\`, "/"))
	if rest == "" {
		return volume + "."
	}
	return volume + strings.ReplaceAll(path.Clean(rest), "/", `\`)
}

func windowsVolume(slashed string) (string, string) {
	if HasDrive(slashed) {
		return strings.ToUpper(slashed[:1]) + ":", slashed[2:]
	}
	if rest, ok := strings.CutPrefix(slashed, "//"); ok {
		host, after, _ := strings.Cut(rest, "/")
		share, tail, _ := strings.Cut(after, "/")
		return `\\` + host + `\` + share, "/" + tail
	}
	return "", slashed
}

func HasDrive(p string) bool {
	return len(p) >= 2 && p[1] == ':' && ('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

func (f Flavour) Fold(p string) string {
	if f == Windows {
		return strings.ToLower(p)
	}
	return p
}

func (f Flavour) Within(p, root string) bool {
	p, root = f.Fold(f.Clean(p)), f.Fold(f.Clean(root))
	rest, ok := strings.CutPrefix(p, root)
	sep := f.Separator()
	return ok && (rest == "" || rest[0] == sep || root[len(root)-1] == sep)
}

func (f Flavour) Executable(name string, mode fs.FileMode, pathExt []string) (string, bool) {
	if !mode.IsRegular() {
		return "", false
	}
	if f == POSIX {
		if mode.Perm()&0o111 == 0 {
			return "", false
		}
		return name, true
	}
	ext := path.Ext(name)
	if ext == "" || !slices.ContainsFunc(pathExt, func(e string) bool { return strings.EqualFold(e, ext) }) {
		return "", false
	}
	return strings.TrimSuffix(name, ext), true
}

const defaultPathExt = ".COM;.EXE;.BAT;.CMD"

func (f Flavour) PathExt(env string) []string {
	if f == POSIX {
		return nil
	}
	if env == "" {
		env = defaultPathExt
	}
	return slices.DeleteFunc(strings.Split(env, ";"), func(ext string) bool { return ext == "" })
}
