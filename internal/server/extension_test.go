package server

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var extensionCommandID = regexp.MustCompile(`:\s*"([^"]+)"`)

func TestVSCodeExtensionKnowsEveryServerCommand(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "..", "editors", "vscode", "src", "ids.ts")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	_, block, found := strings.Cut(string(raw), "export const serverCommands = {\n")
	if !found {
		t.Fatalf("%s declares no serverCommands object", path)
	}
	block, _, found = strings.Cut(block, "\n}")
	if !found {
		t.Fatalf("%s does not close its serverCommands object", path)
	}
	var got []string
	for _, match := range extensionCommandID.FindAllStringSubmatch(block, -1) {
		got = append(got, match[1])
	}
	if want := slices.Sorted(slices.Values(serverCommands)); !slices.Equal(slices.Sorted(slices.Values(got)), want) {
		t.Errorf("%s lists the server commands %v, the server provides %v", path, got, want)
	}
}
