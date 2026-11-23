package gitrepo

import (
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/stubbedev/shell-lsp/internal/platform"
)

type Process struct {
	Variables    map[string]string
	SystemConfig string
	Prefix       func() (string, error)
	UserHome     func(name string) (string, error)
}

type Environment struct {
	User platform.UserDirs
	Git  Process
}

const (
	variablePrefix     = "GIT_CONFIG"
	noSystemVariable   = "GIT_CONFIG_NOSYSTEM"
	systemVariable     = "GIT_CONFIG_SYSTEM"
	globalVariable     = "GIT_CONFIG_GLOBAL"
	parametersVariable = "GIT_CONFIG_PARAMETERS"
	countVariable      = "GIT_CONFIG_COUNT"
	keyVariable        = "GIT_CONFIG_KEY_"
	valueVariable      = "GIT_CONFIG_VALUE_"
	unixSystemConfig   = "/etc/gitconfig"
	executableName     = "git"
	executableDirName  = "bin"
)

var errNotExpanded = errors.New("unknown")

func CurrentProcess(environ, pathDirs, pathExt []string) Process {
	variables := map[string]string{}
	for _, kv := range environ {
		if name, value, ok := strings.Cut(kv, "="); ok && strings.HasPrefix(name, variablePrefix) {
			variables[name] = value
		}
	}
	system := unixSystemConfig
	if platform.Native == platform.Windows {
		system = ""
	}
	return Process{
		Variables:    variables,
		SystemConfig: system,
		Prefix:       sync.OnceValues(func() (string, error) { return installPrefix(pathDirs, pathExt) }),
		UserHome:     userHome,
	}
}

func installPrefix(pathDirs, pathExt []string) (string, error) {
	found, ok := platform.Native.LookPath(executableName, pathDirs, pathExt, os.Stat)
	if !ok {
		return "", errors.New("git is not on PATH, so its installation prefix is unknown")
	}
	real, err := filepath.EvalSymlinks(found)
	if err != nil {
		return "", fmt.Errorf("resolving the git executable %s: %w", found, err)
	}
	bin := filepath.Dir(real)
	if filepath.Base(bin) != executableDirName {
		return "", fmt.Errorf("the git executable %s is not in a %s directory, so its installation prefix is unknown", real, executableDirName)
	}
	return filepath.Dir(bin), nil
}

func userHome(name string) (string, error) {
	u, err := user.Lookup(name)
	if _, ok := errors.AsType[user.UnknownUserError](err); ok {
		return "", fmt.Errorf("the user %q: %w", name, errNotExpanded)
	}
	if err != nil {
		return "", fmt.Errorf("looking up the home directory of %q: %w", name, err)
	}
	return u.HomeDir, nil
}

type scopes struct {
	system      string
	globals     []string
	commandLine []source
}

func newScopes(env Environment) (scopes, error) {
	vars := env.Git.Variables
	var out scopes
	noSystem, err := parseBool(vars[noSystemVariable])
	if err != nil {
		return scopes{}, fmt.Errorf("%s: %w", noSystemVariable, err)
	}
	if !noSystem {
		out.system = env.Git.SystemConfig
		if path, set := vars[systemVariable]; set {
			out.system = path
		}
	}
	if path, set := vars[globalVariable]; set {
		if path != "" {
			out.globals = []string{path}
		}
	} else {
		if dir := env.User.ConfigDir(); dir != "" {
			out.globals = append(out.globals, filepath.Join(dir, "git", "config"))
		}
		if env.User.Home != "" {
			out.globals = append(out.globals, filepath.Join(env.User.Home, ".gitconfig"))
		}
	}
	if out.commandLine, err = countedParameters(vars); err != nil {
		return scopes{}, err
	}
	listed, err := parameters(vars[parametersVariable])
	if err != nil {
		return scopes{}, fmt.Errorf("%s: %w", parametersVariable, err)
	}
	out.commandLine = append(out.commandLine, listed...)
	return out, nil
}

func commandSetting(label, name, value string, hasValue bool) ([]source, error) {
	canonical, err := canonicalKey(name)
	if err != nil {
		return nil, err
	}
	parsed, ok, err := newSetting(canonical, value, hasValue, 0)
	if err != nil || !ok {
		return nil, err
	}
	return []source{{label: label, settings: config{parsed}}}, nil
}

const cSpace = " \t\n\v\f\r"

func isCSpace(c byte) bool { return strings.IndexByte(cSpace, c) >= 0 }

var errBadParameters = errors.New("not a list of quoted settings")

func parameters(text string) ([]source, error) {
	var out []source
	for rest := text; rest != ""; rest = strings.TrimLeft(rest, cSpace) {
		name, after, ok := dequote(rest)
		if !ok {
			return nil, errBadParameters
		}
		value, hasValue := "", true
		switch {
		case after == "" || isCSpace(after[0]):
			name, value, hasValue = strings.Cut(name, "=")
		case after[0] == '=' && (len(after) == 1 || isCSpace(after[1])):
			after, hasValue = after[1:], false
		case after[0] == '=':
			value, after, ok = dequote(after[1:])
			if !ok || after != "" && !isCSpace(after[0]) {
				return nil, errBadParameters
			}
		default:
			return nil, errBadParameters
		}
		parsed, err := commandSetting(fmt.Sprintf("%s entry %q", parametersVariable, name), name, value, hasValue)
		if err != nil {
			return nil, err
		}
		out = append(out, parsed...)
		rest = after
	}
	return out, nil
}

func dequote(text string) (string, string, bool) {
	if !strings.HasPrefix(text, "'") {
		return "", "", false
	}
	var out strings.Builder
	for i := 1; ; {
		end := strings.IndexByte(text[i:], '\'')
		if end < 0 {
			return "", "", false
		}
		out.WriteString(text[i : i+end])
		i += end + 1
		if len(text) >= i+3 && text[i] == '\\' && (text[i+1] == '\'' || text[i+1] == '!') && text[i+2] == '\'' {
			out.WriteByte(text[i+1])
			i += 3
			continue
		}
		return out.String(), text[i:], true
	}
}

func countedParameters(vars map[string]string) ([]source, error) {
	text, set := vars[countVariable]
	if !set {
		return nil, nil
	}
	digits := strings.TrimPrefix(strings.TrimLeft(text, cSpace), "+")
	count := uint64(0)
	if digits != "" {
		var err error
		if count, err = strconv.ParseUint(digits, 10, 64); err != nil {
			return nil, fmt.Errorf("%s=%q is not a count", countVariable, text)
		}
	}
	if count > 1<<31-1 {
		return nil, fmt.Errorf("%s=%q is too many entries", countVariable, text)
	}
	var out []source
	for i := range int(count) {
		keyName, valueName := keyVariable+strconv.Itoa(i), valueVariable+strconv.Itoa(i)
		name, ok := vars[keyName]
		if !ok {
			return nil, fmt.Errorf("%s is not set", keyName)
		}
		value, ok := vars[valueName]
		if !ok {
			return nil, fmt.Errorf("%s is not set", valueName)
		}
		parsed, err := commandSetting(keyName, name, value, true)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", keyName, err)
		}
		out = append(out, parsed...)
	}
	return out, nil
}
