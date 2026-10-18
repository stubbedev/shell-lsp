package hadolint

import (
	"errors"
	"fmt"
	"slices"

	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/yaml"
)

type LabelFormat uint8

const (
	LabelText LabelFormat = iota + 1
	LabelURL
	LabelSemver
	LabelHash
	LabelRFC3339
	LabelSPDX
	LabelEmail
)

var labelFormats = map[string]LabelFormat{
	"text": LabelText, "url": LabelURL, "semver": LabelSemver, "hash": LabelHash,
	"rfc3339": LabelRFC3339, "spdx": LabelSPDX, "email": LabelEmail,
}

type Label struct {
	Name   string
	Format LabelFormat
}

type Config struct {
	Path                string
	Ignored             []lint.Code
	Overrides           map[lint.Code]lint.Severity
	TrustedRegistries   []string
	LabelSchema         []Label
	StrictLabels        bool
	DisableIgnorePragma bool
	Optional            []string
}

var (
	overrideLevels = []string{"error", "warning", "info", "style"}
	thresholds     = []string{"error", "warning", "info", "style", "ignore", "none"}
	formats        = []string{"tty", "json", "checkstyle", "codeclimate", "gitlab_codeclimate", "gnu", "codacy", "sonarqube", "sarif"}
)

type configError struct {
	node    *yaml.Node
	message string
}

func (e *configError) Error() string {
	return fmt.Sprintf("line %d, column %d: %s", e.node.Line, e.node.Column, e.message)
}

func fail(n *yaml.Node, format string, args ...any) error {
	return &configError{node: n, message: fmt.Sprintf(format, args...)}
}

func ParseConfig(path, text string) (Config, error) {
	cfg := Config{Path: path}
	root, err := yaml.Parse(text)
	if err != nil {
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}
	if root.Null() {
		return cfg, nil
	}
	if root.Kind != yaml.Mapping {
		return Config{}, fmt.Errorf("reading %s: %w", path, fail(root, "the configuration is %s, but Hadolint expects a mapping of settings", root.Kind))
	}
	for _, p := range root.Pairs {
		if p.Value.Null() {
			continue
		}
		if err := cfg.set(p.Key, p.Value); err != nil {
			return Config{}, fmt.Errorf("reading %s: %w", path, err)
		}
	}
	return cfg, nil
}

func (cfg *Config) set(key, value *yaml.Node) error {
	switch key.Value {
	case "ignored":
		codes, err := codeList(value)
		cfg.Ignored = codes
		return err
	case "override":
		return cfg.override(value)
	case "trustedRegistries":
		registries, err := stringList(value)
		cfg.TrustedRegistries = registries
		return err
	case "label-schema":
		return cfg.labels(value)
	case "strict-labels":
		return boolean(value, &cfg.StrictLabels)
	case "disable-ignore-pragma":
		return boolean(value, &cfg.DisableIgnorePragma)
	case "no-fail", "no-color", "verbose":
		var ignored bool
		return boolean(value, &ignored)
	case "failure-threshold":
		return oneOf(value, thresholds)
	case "format":
		return oneOf(value, formats)
	}
	return fail(key, "unknown setting %q; Hadolint reads ignored, override, failure-threshold, no-fail, trustedRegistries, label-schema, strict-labels, disable-ignore-pragma, format, no-color and verbose", key.Value)
}

func scalar(n *yaml.Node, what string) (string, error) {
	if n.Kind != yaml.Scalar {
		return "", fail(n, "expected %s, found %s", what, n.Kind)
	}
	return n.Value, nil
}

func stringList(n *yaml.Node) ([]string, error) {
	if n.Kind != yaml.Sequence {
		return nil, fail(n, "expected a list, found %s", n.Kind)
	}
	out := make([]string, 0, len(n.Items))
	for _, item := range n.Items {
		s, err := scalar(item, "a string")
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func codeList(n *yaml.Node) ([]lint.Code, error) {
	texts, err := stringList(n)
	if err != nil {
		return nil, err
	}
	out := make([]lint.Code, 0, len(texts))
	for i, text := range texts {
		if text == "" || text[0] >= '0' && text[0] <= '9' {
			return nil, fail(n.Items[i], "expected a check code with its prefix, as in DL%s or SC%s, found %q", text, text, text)
		}
		code, err := lint.ParseCode(text)
		if err != nil {
			return nil, fail(n.Items[i], "%v", err)
		}
		out = append(out, code)
	}
	return out, nil
}

func boolean(n *yaml.Node, out *bool) error {
	b, ok := n.Bool()
	if !ok {
		return fail(n, "expected true or false, found %q", n.Value)
	}
	*out = b
	return nil
}

func oneOf(n *yaml.Node, allowed []string) error {
	s, err := scalar(n, "a string")
	if err != nil {
		return err
	}
	if !slices.Contains(allowed, s) {
		return fail(n, "%q is none of %v", s, allowed)
	}
	return nil
}

func (cfg *Config) override(n *yaml.Node) error {
	if n.Kind != yaml.Mapping {
		return fail(n, "expected a mapping of levels to codes, found %s", n.Kind)
	}
	cfg.Overrides = map[lint.Code]lint.Severity{}
	for _, p := range n.Pairs {
		if !slices.Contains(overrideLevels, p.Key.Value) {
			return fail(p.Key, "unknown level %q; override takes error, warning, info and style", p.Key.Value)
		}
		if p.Value.Null() {
			continue
		}
		severity, _ := lint.SeverityNamed(p.Key.Value)
		codes, err := codeList(p.Value)
		if err != nil {
			return err
		}
		for _, code := range codes {
			if current, set := cfg.Overrides[code]; !set || severity < current {
				cfg.Overrides[code] = severity
			}
		}
	}
	return nil
}

func (cfg *Config) labels(n *yaml.Node) error {
	if n.Kind != yaml.Mapping {
		return fail(n, "expected a mapping of label names to formats, found %s", n.Kind)
	}
	for _, p := range n.Pairs {
		name, err := scalar(p.Value, "a label format")
		if err != nil {
			return err
		}
		format, ok := labelFormats[name]
		if !ok {
			return fail(p.Value, "unknown label format %q; Hadolint knows text, url, semver, hash, rfc3339, spdx and email", name)
		}
		cfg.LabelSchema = append(cfg.LabelSchema, Label{Name: p.Key.Value, Format: format})
	}
	return nil
}

func IsConfigError(err error) bool {
	var ce *configError
	var ye *yaml.Error
	return errors.As(err, &ce) || errors.As(err, &ye)
}

func (cfg Config) Enables(code lint.Code) bool {
	_, ok := cfg.Overrides[code]
	return ok
}
