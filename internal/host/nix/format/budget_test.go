package format

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

const maxBytesPerSourceByte = 230

func largePackageSet(packages int) string {
	var sb strings.Builder
	sb.WriteString("{ mkDerivation, fetchurl }:\n{\n")
	for i := range packages {
		fmt.Fprintf(&sb, "  pkg%d = { mkDerivation, base, bytes, containers, text }: mkDerivation {\n    pname = \"pkg%d\";\n    version = \"1.0.%d\";\n    sha256 = \"0000000000000000000000000000000000000000000000000000\";\n    libraryHaskellDepends = [ base bytes containers text ];\n    homepage = \"https://example.org/pkg%d\";\n    description = \"A package\";\n    license = lib.licenses.bsd3;\n  };\n", i, i, i, i)
	}
	sb.WriteString("}\n")
	return sb.String()
}

func TestFormattingAHugeFileStaysInItsAllocationBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("allocation budgets are measured in full test runs")
	}
	src := largePackageSet(2000)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	if _, err := Format(src); err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	perByte := (after.TotalAlloc - before.TotalAlloc) / uint64(len(src))
	t.Logf("%d bytes allocated per source byte", perByte)
	if perByte > maxBytesPerSourceByte {
		t.Errorf("formatting allocated %d bytes per source byte, over the budget of %d", perByte, maxBytesPerSourceByte)
	}
}
