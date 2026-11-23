package bake

import (
	"cmp"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
)

const (
	attrContext       = "context"
	attrDockerfile    = "dockerfile"
	attrContexts      = "contexts"
	attrInherits      = "inherits"
	defaultContext    = "."
	defaultDockerfile = "Dockerfile"
	imageScheme       = "docker-image://"
	hclSuffix         = ".hcl"
	sshPrefix         = "git@"
	schemeSeparator   = "://"
)

var (
	composeNames = []string{"compose.yaml", "compose.yml", "docker-compose.yml", "docker-compose.yaml"}
	jsonNames    = []string{"docker-bake.json", "docker-bake.override.json"}
	hclNames     = []string{"docker-bake.hcl", "docker-bake.override.hcl"}
	defaultNames = slices.Concat(composeNames, jsonNames, hclNames)
)

var Patterns = slices.Concat(composeNames, jsonNames, []string{"*" + hclSuffix})

func IsName(path string) bool {
	base := filepath.Base(path)
	return slices.Contains(defaultNames, base) || strings.HasSuffix(base, hclSuffix)
}

type Value struct {
	Text  string
	Known bool
}

func (v Value) Image() (string, bool) {
	if !v.Known {
		return "", false
	}
	return strings.CutPrefix(v.Text, imageScheme)
}

type target struct {
	Name       string
	Context    *Value
	Dockerfile *Value
	Contexts   map[string]Value
	Inherits   []string
}

type Contexts map[string][]Value

type Dockerfiles map[string]Contexts

func (d Dockerfiles) Of(path string) Contexts { return d[filepath.Clean(path)] }

func (d Dockerfiles) add(path string, contexts map[string]Value) {
	merged := d[path]
	if merged == nil {
		merged = Contexts{}
		d[path] = merged
	}
	for name, v := range contexts {
		if slices.Contains(merged[name], v) {
			continue
		}
		merged[name] = append(merged[name], v)
		slices.SortFunc(merged[name], func(a, b Value) int {
			return cmp.Or(strings.Compare(a.Text, b.Text), boolOrder(a.Known, b.Known))
		})
	}
}

func boolOrder(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}

func parse(path string, data []byte) ([]target, error) {
	base := filepath.Base(path)
	text := string(data)
	var targets []target
	var err error
	switch {
	case slices.Contains(composeNames, base):
		targets, err = parseCompose(text)
		if err != nil && !strings.Contains(text, composeContexts) {
			return nil, nil
		}
	case slices.Contains(jsonNames, base):
		targets, err = parseJSON(text)
	case slices.Contains(hclNames, base):
		targets, err = parseHCL(text)
	case strings.HasSuffix(base, hclSuffix):
		if targets, err = parseHCL(text); err != nil {
			return nil, nil
		}
	default:
		return nil, fmt.Errorf("reading %s: %s is no bake file name", path, base)
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return targets, nil
}

func projects(names []string) [][]string {
	var defaults []string
	for _, name := range defaultNames {
		if slices.Contains(names, name) {
			defaults = append(defaults, name)
		}
	}
	var out [][]string
	if len(defaults) > 0 {
		out = append(out, defaults)
	}
	for _, name := range names {
		if !slices.Contains(defaultNames, name) {
			out = append(out, []string{name})
		}
	}
	return out
}

func overlay(base, top target) target {
	out := base
	out.Name = top.Name
	out.Inherits = top.Inherits
	if top.Context != nil {
		out.Context = top.Context
	}
	if top.Dockerfile != nil {
		out.Dockerfile = top.Dockerfile
	}
	out.Contexts = make(map[string]Value, len(base.Contexts)+len(top.Contexts))
	maps.Copy(out.Contexts, base.Contexts)
	maps.Copy(out.Contexts, top.Contexts)
	return out
}

func merge(targets []target) ([]string, map[string]target) {
	var order []string
	byName := map[string]target{}
	for _, t := range targets {
		earlier, seen := byName[t.Name]
		if !seen {
			order = append(order, t.Name)
			byName[t.Name] = overlay(target{}, t)
			continue
		}
		inherits := earlier.Inherits
		merged := overlay(earlier, t)
		if t.Inherits == nil {
			merged.Inherits = inherits
		}
		byName[t.Name] = merged
	}
	return order, byName
}

func inherit(t target, byName map[string]target, seen []string) target {
	if slices.Contains(seen, t.Name) {
		return t
	}
	seen = append(seen, t.Name)
	out := target{}
	for _, name := range t.Inherits {
		if parent, ok := byName[name]; ok {
			out = overlay(out, inherit(parent, byName, seen))
		}
	}
	return overlay(out, t)
}

func remote(path string) bool {
	return strings.Contains(path, schemeSeparator) || strings.HasPrefix(path, sshPrefix)
}

func within(dir, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(dir, path)
}

func dockerfile(dir string, t target) (string, bool) {
	context, file := Value{Text: defaultContext, Known: true}, Value{Text: defaultDockerfile, Known: true}
	if t.Context != nil {
		context = *t.Context
	}
	if t.Dockerfile != nil {
		file = *t.Dockerfile
	}
	if !context.Known || !file.Known || remote(context.Text) {
		return "", false
	}
	return within(within(dir, context.Text), file.Text), true
}

func builds(dir string, targets []target, into Dockerfiles) {
	order, byName := merge(targets)
	for _, name := range order {
		t := inherit(byName[name], byName, nil)
		if len(t.Contexts) == 0 {
			continue
		}
		if path, ok := dockerfile(dir, t); ok {
			into.add(path, t.Contexts)
		}
	}
}
