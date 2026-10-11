package config

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/dialect"
)

var extensionOnlySettings = []string{Section + ".path", Section + ".trace.server"}

type contributedSetting struct {
	Type        json.RawMessage   `json:"type"`
	Default     json.RawMessage   `json:"default"`
	Scope       string            `json:"scope"`
	Description string            `json:"markdownDescription"`
	Enum        []json.RawMessage `json:"enum"`
	Minimum     *float64          `json:"minimum"`
	Items       *struct {
		Type string `json:"type"`
	} `json:"items"`
}

func contributedSettings(t *testing.T) map[string]contributedSetting {
	t.Helper()

	settings := allContributedSettings(t)
	for _, key := range extensionOnlySettings {
		delete(settings, key)
	}
	return settings
}

func allContributedSettings(t *testing.T) map[string]contributedSetting {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "editors", "vscode", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Contributes struct {
			Configuration []struct {
				Properties map[string]contributedSetting `json:"properties"`
			} `json:"configuration"`
		} `json:"contributes"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parsing the VS Code extension manifest: %v", err)
	}
	settings := map[string]contributedSetting{}
	for _, category := range manifest.Contributes.Configuration {
		maps.Copy(settings, category.Properties)
	}
	return settings
}

func serverSettingTypes(prefix string, t reflect.Type, out map[string]reflect.Type) {
	for field := range t.Fields() {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		key := prefix + "." + name
		if field.Type.Kind() == reflect.Pointer && field.Type.Elem().Kind() == reflect.Struct {
			serverSettingTypes(key, field.Type.Elem(), out)
			continue
		}
		out[key] = field.Type
	}
}

func schemaType(field reflect.Type) string {
	switch field {
	case reflect.TypeFor[*bool]():
		return `"boolean"`
	case reflect.TypeFor[*uint]():
		return `["integer","null"]`
	case reflect.TypeFor[*string]():
		return `["string","null"]`
	case reflect.TypeFor[[]string]():
		return `"array"`
	}
	return "unmapped Go type " + field.String()
}

func compactJSON(t *testing.T, raw json.RawMessage) string {
	t.Helper()

	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatalf("parsing %s: %v", raw, err)
	}
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestVSCodeExtensionContributesEverySetting(t *testing.T) {
	t.Parallel()

	contributed := contributedSettings(t)
	server := map[string]reflect.Type{}
	serverSettingTypes(Section, reflect.TypeFor[rawSettings](), server)

	if got, want := slices.Sorted(maps.Keys(contributed)), slices.Sorted(maps.Keys(server)); !slices.Equal(got, want) {
		t.Fatalf("the extension contributes %v, the server reads %v", got, want)
	}
	for key, field := range server {
		setting := contributed[key]
		if got, want := compactJSON(t, setting.Type), schemaType(field); got != want {
			t.Errorf("%s has type %s, want %s", key, got, want)
		}
		if field == reflect.TypeFor[[]string]() && (setting.Items == nil || setting.Items.Type != "string") {
			t.Errorf("%s does not declare string items", key)
		}
		if field == reflect.TypeFor[*uint]() && (setting.Minimum == nil || *setting.Minimum != 0) {
			t.Errorf("%s does not declare minimum 0", key)
		}
	}
}

func TestVSCodeExtensionOffersEveryDialect(t *testing.T) {
	t.Parallel()

	var want []string
	for d := dialect.Bash; dialect.All.Has(d); d++ {
		want = append(want, `"`+d.String()+`"`)
	}
	var got []string
	for _, value := range contributedSettings(t)[Section+".dialect"].Enum {
		if string(value) != "null" {
			got = append(got, string(value))
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("dialect enum = %v, want %v", got, want)
	}
	for _, name := range got {
		var plain string
		if err := json.Unmarshal([]byte(name), &plain); err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(json.RawMessage(`{"dialect":` + name + `}`)); err != nil {
			t.Errorf("the server rejects the offered dialect %s: %v", plain, err)
		}
	}
}

func TestVSCodeExtensionDefaultsMatchTheServer(t *testing.T) {
	t.Parallel()

	defaults := map[string]any{}
	for key, setting := range contributedSettings(t) {
		path := strings.Split(strings.TrimPrefix(key, Section+"."), ".")
		target := defaults
		for _, part := range path[:len(path)-1] {
			child, ok := target[part].(map[string]any)
			if !ok {
				child = map[string]any{}
				target[part] = child
			}
			target = child
		}
		target[path[len(path)-1]] = setting.Default
	}
	raw, err := json.Marshal(defaults)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Parse(raw)
	if err != nil {
		t.Fatalf("the server rejects the extension's defaults %s: %v", raw, err)
	}
	want := Default()
	if got.Dialect != want.Dialect || got.Lint.Enabled != want.Lint.Enabled || len(got.Lint.Exclude) != 0 || len(got.Lint.Optional) != 0 {
		t.Errorf("defaults %s parse to %+v, want %+v", raw, got, want)
	}
	if got.Format.Enabled != want.Format.Enabled || got.Format.Style.Options() != want.Format.Style.Options() {
		t.Errorf("format defaults %s parse to %+v, want %+v", raw, got.Format.Style.Options(), want.Format.Style.Options())
	}
	if got.Format.Style.Indent != nil {
		t.Errorf("format.indent defaults to %d, want unset so the editor's tab size applies", *got.Format.Style.Indent)
	}
}

type documentedSetting struct {
	key, defaultValue, meaning string
}

func settingsTable(t *testing.T, path string) []documentedSetting {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, table, found := strings.Cut(string(raw), "| Default | Meaning |\n")
	if !found {
		t.Fatalf("%s has no settings table", path)
	}
	table, _, _ = strings.Cut(table, "\n\n")
	var out []documentedSetting
	for row := range strings.SplitSeq(table, "\n") {
		if strings.HasPrefix(row, "| ---") {
			continue
		}
		cells := strings.Split(strings.ReplaceAll(strings.Trim(row, "| "), `\|`, "\x00"), " | ")
		if len(cells) != 3 {
			t.Fatalf("%s: settings row %q does not have three cells", path, row)
		}
		unescape := func(s string) string { return strings.ReplaceAll(s, "\x00", "|") }
		out = append(out, documentedSetting{key: strings.Trim(cells[0], "`"), defaultValue: unescape(cells[1]), meaning: unescape(cells[2])})
	}
	return out
}

func describes(description, meaning string) bool {
	rest, ok := strings.CutPrefix(description, meaning)
	return ok && (rest == "." || strings.HasPrefix(rest, ". ") || strings.HasPrefix(rest, "; "))
}

func checkSettingsTable(t *testing.T, path, prefix string, want map[string]contributedSetting) {
	t.Helper()

	documented := settingsTable(t, path)
	keys := make([]string, 0, len(documented))
	for _, row := range documented {
		key := prefix + row.key
		keys = append(keys, key)
		setting, ok := want[key]
		if !ok {
			continue
		}
		if !describes(setting.Description, row.meaning) {
			t.Errorf("%s describes %s as %q, but the extension says %q", path, row.key, row.meaning, setting.Description)
		}
		defaultValue := compactJSON(t, setting.Default)
		var plain string
		if json.Unmarshal(setting.Default, &plain) == nil && "`"+plain+"`" == row.defaultValue {
			continue
		}
		if defaultValue == "null" {
			if strings.HasPrefix(row.defaultValue, "`") {
				t.Errorf("%s gives %s the default %s, but it is unset by default", path, row.key, row.defaultValue)
			}
			continue
		}
		if row.defaultValue != "`"+defaultValue+"`" {
			t.Errorf("%s gives %s the default %s, the extension %s", path, row.key, row.defaultValue, defaultValue)
		}
	}
	if got, all := slices.Sorted(slices.Values(keys)), slices.Sorted(maps.Keys(want)); !slices.Equal(got, all) {
		t.Errorf("%s documents %v, the settings are %v", path, got, all)
	}
}

func TestReadmesDocumentEverySettingAsTheExtensionDoes(t *testing.T) {
	t.Parallel()

	checkSettingsTable(t, filepath.Join("..", "..", "README.md"), Section+".", contributedSettings(t))
	checkSettingsTable(t, filepath.Join("..", "..", "editors", "vscode", "README.md"), "", allContributedSettings(t))
}

func TestServerSettingsCanDifferPerWorkspaceFolder(t *testing.T) {
	t.Parallel()

	for key, setting := range allContributedSettings(t) {
		perFolder := setting.Scope == "resource"
		if perFolder == slices.Contains(extensionOnlySettings, key) {
			t.Errorf("%s has scope %q; server settings are per folder (resource) and extension-only ones are not", key, setting.Scope)
		}
	}
}
