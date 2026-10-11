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
	cleaned := volume + strings.ReplaceAll(path.Clean(rest), "/", `\`)
	if volume == "" && HasDrive(cleaned) {
		return `.\` + cleaned
	}
	return cleaned
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
	return len(p) >= 2 && p[1] == ':' && isASCIILetter(p[0])
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func (f Flavour) IsAbs(p string) bool {
	if f == POSIX {
		return strings.HasPrefix(p, "/")
	}
	slashed := strings.ReplaceAll(p, `\`, "/")
	return strings.HasPrefix(slashed, "//") || HasDrive(slashed) && len(slashed) > 2 && slashed[2] == '/'
}

func (f Flavour) Join(dir, p string) string {
	return f.Clean(dir + string(f.Separator()) + p)
}

func (f Flavour) FromMSYS(p string) string {
	if f == POSIX || len(p) < 2 || p[0] != '/' || !isASCIILetter(p[1]) || len(p) > 2 && p[2] != '/' {
		return p
	}
	return f.Clean(p[1:2] + ":/" + strings.TrimPrefix(p[2:], "/"))
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

func (f Flavour) MayExecute(mode fs.FileMode) bool {
	return mode.IsRegular() && (f == Windows || mode.Perm()&0o111 != 0)
}

func (f Flavour) Executable(name string, mode fs.FileMode, pathExt []string) (string, bool) {
	if !f.MayExecute(mode) {
		return "", false
	}
	if f == POSIX {
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
