package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
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

var (
	repoName   = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]*$`)
	repoURL    = regexp.MustCompile(`^https://github\.com/[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	commitHash = regexp.MustCompile(`^[0-9a-f]{40}$`)
	license    = regexp.MustCompile(`^[A-Za-z0-9.+-]+$`)
)

const manifestFields = 5

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
	if !repoName.MatchString(name) {
		return Repo{}, fmt.Errorf("repository name %q is not lower-case letters, digits, '.', '_' or '-'", name)
	}
	kind, ok := kindFromName(kindName)
	if !ok {
		return Repo{}, fmt.Errorf("%s: kind %q is not one of %s", name, kindName, strings.Join(kindNames[:], ", "))
	}
	if !license.MatchString(lic) {
		return Repo{}, fmt.Errorf("%s: licence %q is not an SPDX identifier", name, lic)
	}
	if !repoURL.MatchString(url) {
		return Repo{}, fmt.Errorf("%s: URL %q is not https://github.com/OWNER/REPO", name, url)
	}
	if !commitHash.MatchString(commit) {
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
	return hex.EncodeToString(sum[:6])
}

func (r Repo) dirName() string { return r.Name + "@" + r.key() }
