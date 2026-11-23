package bake

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/yaml"
)

const (
	composeServices   = "services"
	composeBuild      = "build"
	composeContext    = "context"
	composeDockerfile = "dockerfile"
	composeContexts   = "additional_contexts"
	composeEscape     = "$$"
	composeVariable   = "$"
	contextSeparator  = "="
)

type jsonFile struct {
	Target   map[string]jsonTarget   `json:"target"`
	Variable map[string]jsonVariable `json:"variable"`
}

type jsonVariable struct {
	Default any `json:"default"`
}

type jsonTarget struct {
	Context    *string           `json:"context"`
	Dockerfile *string           `json:"dockerfile"`
	Contexts   map[string]string `json:"contexts"`
	Inherits   []string          `json:"inherits"`
}

func parseJSON(text string) (file, error) {
	var raw jsonFile
	if err := json.Unmarshal([]byte(text), &raw); err != nil {
		return file{}, err
	}
	out := file{Targets: make([]target, 0, len(raw.Target)), Variables: make(variables, len(raw.Variable))}
	for _, name := range slices.Sorted(maps.Keys(raw.Target)) {
		rt := raw.Target[name]
		t := target{Name: name, Context: literalOf(rt.Context), Dockerfile: literalOf(rt.Dockerfile), Inherits: rt.Inherits}
		if rt.Contexts != nil {
			t.Contexts = make(map[string]template, len(rt.Contexts))
			for key, v := range rt.Contexts {
				t.Contexts[key] = literalText(v)
			}
		}
		out.Targets = append(out.Targets, t)
	}
	for name, v := range raw.Variable {
		if v.Default == nil {
			continue
		}
		text, isString := v.Default.(string)
		if !isString {
			out.Variables[name] = unknown
			continue
		}
		out.Variables[name] = literalText(text)
	}
	return out, nil
}

func literalOf(text *string) *template {
	if text == nil {
		return nil
	}
	t := literalText(*text)
	return &t
}

func literalText(text string) template {
	var b templateBuilder
	for i := 0; i < len(text); i++ {
		escaped, interpolates := templateMarker(text[i:])
		switch {
		case escaped != "":
			b.text(escaped)
			i += escapedTemplate - 1
		case interpolates:
			end := strings.IndexByte(text[i:], '}')
			if end < 0 || !strings.HasPrefix(text[i:], interpolation) {
				return unknown
			}
			name := strings.TrimSpace(text[i+len(interpolation) : i+end])
			if !identifier(name) {
				return unknown
			}
			b.reference(name)
			i += end
		default:
			b.text(text[i : i+1])
		}
	}
	return b.build()
}

func parseCompose(text string) ([]target, error) {
	root, err := yaml.Parse(text)
	if err != nil {
		return nil, err
	}
	if root.Kind != yaml.Mapping {
		return nil, nil
	}
	services, ok := root.Get(composeServices)
	if !ok || services.Kind != yaml.Mapping {
		return nil, nil
	}
	var out []target
	for _, service := range services.Pairs {
		build, ok := service.Value.Get(composeBuild)
		if !ok {
			continue
		}
		t := target{Name: service.Key.Value}
		switch build.Kind {
		case yaml.Scalar:
			t.Context = composeValue(build)
		case yaml.Mapping:
			t.Context = composeField(build, composeContext)
			t.Dockerfile = composeField(build, composeDockerfile)
			if contexts, ok := build.Get(composeContexts); ok {
				t.Contexts = composeContextMap(contexts)
			}
		case yaml.Sequence:
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

func composeField(n *yaml.Node, key string) *template {
	field, ok := n.Get(key)
	if !ok || field.Null() {
		return nil
	}
	return composeValue(field)
}

func composeValue(n *yaml.Node) *template {
	if n.Kind != yaml.Scalar {
		t := unknown
		return &t
	}
	t := composeText(n.Value)
	return &t
}

func composeText(text string) template {
	if strings.Contains(strings.ReplaceAll(text, composeEscape, ""), composeVariable) {
		return unknown
	}
	return plainTemplate(strings.ReplaceAll(text, composeEscape, composeVariable))
}

func composeContextMap(n *yaml.Node) map[string]template {
	out := map[string]template{}
	switch n.Kind {
	case yaml.Mapping:
		for _, p := range n.Pairs {
			out[p.Key.Value] = *composeValue(p.Value)
		}
	case yaml.Sequence:
		for _, item := range n.Items {
			if item.Kind != yaml.Scalar {
				continue
			}
			if name, value, ok := strings.Cut(item.Value, contextSeparator); ok {
				out[name] = composeText(value)
			}
		}
	case yaml.Scalar:
	}
	return out
}
