package lint

import "testing"

func TestLiteralEqualsFixes(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"ports=(0=http)\n":                "ports=(\"0=http\")\n",
		"days=([0] = sunday)\n":           "days=([0]=sunday)\n",
		"days=([0] =sunday [1]=monday)\n": "days=([0]=sunday [1]=monday)\n",
		"days=([0]  =  sunday)\n":         "days=([0]=sunday)\n",
		"declare -a q=(1=one two)\n":      "declare -a q=(\"1=one\" two)\n",
	}
	for src, want := range tests {
		if got := fixed(t, src, 2191); got != want {
			t.Errorf("fix of %q = %q, want %q", src, got, want)
		}
	}
}

func TestDecodeANSIC(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		`\t`:       "\t",
		`a\nb`:     "a\nb",
		`\x41\101`: "AA",
		`\q`:       `\q`,
		`\\n`:      `\n`,
	}
	for in, want := range tests {
		if got := decodeANSIC(in); got != want {
			t.Errorf("decodeANSIC(%q) = %q, want %q", in, got, want)
		}
	}
}
