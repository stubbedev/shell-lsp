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
	Target map[string]jsonTarget `json:"target"`
}

type jsonTarget struct {
	Context    *string           `json:"context"`
	Dockerfile *string           `json:"dockerfile"`
	Contexts   map[string]string `json:"contexts"`
	Inherits   []string          `json:"inherits"`
}

func parseJSON(text string) ([]target, error) {
	var file jsonFile
	if err := json.Unmarshal([]byte(text), &file); err != nil {
		return nil, err
	}
	out := make([]target, 0, len(file.Target))
	for _, name := range slices.Sorted(maps.Keys(file.Target)) {
		raw := file.Target[name]
		t := target{Name: name, Context: literalOf(raw.Context), Dockerfile: literalOf(raw.Dockerfile), Inherits: raw.Inherits}
		if raw.Contexts != nil {
			t.Contexts = make(map[string]Value, len(raw.Contexts))
			for key, v := range raw.Contexts {
				t.Contexts[key] = literalText(v)
			}
		}
		out = append(out, t)
	}
	return out, nil
}

func literalOf(text *string) *Value {
	if text == nil {
		return nil
	}
	v := literalText(*text)
	return &v
}

func literalText(text string) Value {
	var sb strings.Builder
	for i := 0; i < len(text); i++ {
		escaped, interpolates := templateMarker(text[i:])
		switch {
		case escaped != "":
			sb.WriteString(escaped)
			i += escapedTemplate - 1
		case interpolates:
			return Value{}
		default:
			sb.WriteByte(text[i])
		}
	}
	return Value{Text: sb.String(), Known: true}
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

func composeField(n *yaml.Node, key string) *Value {
	field, ok := n.Get(key)
	if !ok || field.Null() {
		return nil
	}
	return composeValue(field)
}

func composeValue(n *yaml.Node) *Value {
	if n.Kind != yaml.Scalar {
		return &Value{}
	}
	v := composeText(n.Value)
	return &v
}

func composeText(text string) Value {
	if strings.Contains(strings.ReplaceAll(text, composeEscape, ""), composeVariable) {
		return Value{}
	}
	return Value{Text: strings.ReplaceAll(text, composeEscape, composeVariable), Known: true}
}

func composeContextMap(n *yaml.Node) map[string]Value {
	out := map[string]Value{}
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
