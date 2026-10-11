package lint

import (
	"strings"
	"testing"
)

func TestRedirectedComparisonNamesTheOperatorAsWritten(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		script string
		op     string
		width  int
	}{
		{"#!/bin/sh\n[ \"$1\" >= 3 ] && echo big\n", ">=", 2},
		{"#!/bin/sh\n[ \"$1\" <= 3 ] && echo small\n", "<=", 2},
		{"#!/bin/sh\n[ \"$1\" > 3 ] && echo big\n", ">", 1},
		{"#!/bin/sh\n[ \"$1\" > =3 ] && echo big\n", ">", 1},
	} {
		f := findingOf(t, tt.script, 3012)
		if !strings.Contains(f.Message, "with "+tt.op+" ") || f.Span.End-f.Span.Start != tt.width {
			t.Errorf("%q: SC3012 %q over %d bytes, want the operator %s over %d", tt.script, f.Message, f.Span.End-f.Span.Start, tt.op, tt.width)
		}
	}
}
