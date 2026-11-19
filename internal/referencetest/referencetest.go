package referencetest

import (
	"os"
	"testing"
)

func Unavailable(tb testing.TB, message string) {
	tb.Helper()
	if os.Getenv("REQUIRE_REFERENCES") == "1" {
		tb.Fatal(message)
	}
	tb.Skip(message)
}
