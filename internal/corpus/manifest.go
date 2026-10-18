package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/scan"
)

type Kind uint8

const (
	KindScripts Kind = iota
	KindOilsSpec
	KindHosts
)

var kindNames = [...]string{KindScripts: "scripts", KindOilsSpec: "oils-spec", KindHosts: "hosts"}

func (k Kind) String() string { return kindNames[k] }

func kindFromName(name string) (Kind, bool) {
	i := slices.Index(kindNames[:], name)
	return Kind(i), i >= 0 //nolint:gosec
}

type Repo struct {
	Name    string
	Kind    Kind
	License string
	URL     string
	Commit  string
	Paths   []string
}

const (
	manifestFields = 5
	githubPrefix   = "https://github.com/"
	commitDigits   = 40
	keyBytes       = 6
	keySeparator   = "@"
)

func repoName(name string) bool {
	return name != "" && scan.LowerOrDigit(name[0]) && scan.All(name[1:], func(c byte) bool {
		return scan.LowerOrDigit(c) || c == '.' || c == '_' || c == '-'
	})
}

func license(id string) bool {
	return id != "" && scan.All(id, func(c byte) bool { return scan.Alnum(c) || c == '.' || c == '+' || c == '-' })
}

func repoURL(url string) bool {
	path, prefixed := strings.CutPrefix(url, githubPrefix)
	owner, repo, split := strings.Cut(path, "/")
	return prefixed && split && githubName(owner) && githubName(repo)
}

func githubName(name string) bool {
	return name != "" && scan.All(name, func(c byte) bool { return scan.Alnum(c) || c == '_' || c == '.' || c == '-' })
}

func commitHash(commit string) bool {
	return len(commit) == commitDigits && scan.All(commit, scan.LowerHexDigit)
}

func LoadManifest(file string) ([]Repo, error) {
	raw, err := os.ReadFile(file) //nolint:gosec
	if err != nil {
		return nil, fmt.Errorf("reading the corpus manifest: %w", err)
	}
	repos, err := ParseManifest(string(raw))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	return repos, nil
}

func ParseManifest(text string) ([]Repo, error) {
	var repos []Repo
	seen := map[string]bool{}
	for n, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		repo, err := parseRepo(fields)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", n+1, err)
		}
		if seen[repo.Name] {
			return nil, fmt.Errorf("line %d: repository name %q is used twice", n+1, repo.Name)
		}
		seen[repo.Name] = true
		repos = append(repos, repo)
	}
	if len(repos) == 0 {
		return nil, errors.New("no repositories listed")
	}
	return repos, nil
}

func parseRepo(fields []string) (Repo, error) {
	if len(fields) < manifestFields {
		return Repo{}, fmt.Errorf("want name, kind, licence, URL and commit, got %q", strings.Join(fields, " "))
	}
	name, kindName, lic, url, commit := fields[0], fields[1], fields[2], fields[3], fields[4]
	if !repoName(name) {
		return Repo{}, fmt.Errorf("repository name %q is not lower-case letters, digits, '.', '_' or '-'", name)
	}
	kind, ok := kindFromName(kindName)
	if !ok {
		return Repo{}, fmt.Errorf("%s: kind %q is not one of %s", name, kindName, strings.Join(kindNames[:], ", "))
	}
	if !license(lic) {
		return Repo{}, fmt.Errorf("%s: licence %q is not an SPDX identifier", name, lic)
	}
	if !repoURL(url) {
		return Repo{}, fmt.Errorf("%s: URL %q is not https://github.com/OWNER/REPO", name, url)
	}
	if !commitHash(commit) {
		return Repo{}, fmt.Errorf("%s: commit %q is not a full lower-case SHA-1", name, commit)
	}
	paths := fields[manifestFields:]
	for _, p := range paths {
		if p != path.Clean(p) || path.IsAbs(p) || p == "." || strings.HasPrefix(p, "../") || strings.ContainsAny(p, "*?[\\!#") {
			return Repo{}, fmt.Errorf("%s: path %q is not a clean relative path without patterns", name, p)
		}
	}
	return Repo{Name: name, Kind: kind, License: lic, URL: url, Commit: commit, Paths: paths}, nil
}

func (r Repo) key() string {
	sum := sha256.Sum256([]byte(strings.Join(append([]string{r.URL, r.Commit}, r.Paths...), "\n")))
	return hex.EncodeToString(sum[:keyBytes])
}

func (r Repo) dirName() string { return r.Name + keySeparator + r.key() }

func cacheEntry(name string) bool {
	if strings.HasPrefix(name, fetchPrefix) {
		return true
	}
	repo, key, ok := strings.Cut(name, keySeparator)
	return ok && repoName(repo) && len(key) == hex.EncodedLen(keyBytes) && scan.All(key, scan.LowerHexDigit)
}
