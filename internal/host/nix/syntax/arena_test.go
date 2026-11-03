package syntax

import "testing"

func TestChunksHandOutSeparateValues(t *testing.T) {
	t.Parallel()

	var c Chunks[int]
	seen := map[*int]bool{}
	for i := range 3 * maxChunk {
		at := c.New()
		if seen[at] {
			t.Fatalf("value %d shares its address with an earlier one", i)
		}
		seen[at] = true
	}
}

func TestChunksTakeCannotGrowIntoTheNextSlice(t *testing.T) {
	t.Parallel()

	var c Chunks[int]
	first := c.Take(2)
	second := c.Take(1)
	if len(first) != 2 || cap(first) != 2 {
		t.Errorf("first has len %d and cap %d, want 2 and 2", len(first), cap(first))
	}
	second[0] = 7
	first = append(first, 9)
	if second[0] != 7 || first[2] != 9 {
		t.Errorf("appending to the first slice wrote into the second: %v %v", first, second)
	}
	if big := c.Take(2 * maxChunk); len(big) != 2*maxChunk {
		t.Errorf("a request above the chunk size got %d values", len(big))
	}
}

func TestDroppingWhitespaceKeepsEveryOtherTokenFindable(t *testing.T) {
	t.Parallel()

	src := "{ a = 1; # one\n  b = 2; }\n"
	f := Parse(src)
	total := len(f.Tokens)
	var kept []Token
	for _, tok := range f.Tokens {
		if tok.Kind != TokWhitespace {
			kept = append(kept, tok)
		}
	}
	f.DropWhitespaceTokens()
	if len(f.Tokens) != len(kept) || len(kept) >= total {
		t.Fatalf("kept %d of %d tokens, want %d", len(f.Tokens), total, len(kept))
	}
	for i, tok := range kept {
		if f.Tokens[i] != tok {
			t.Errorf("token %d is %v, want %v", i, f.Tokens[i], tok)
		}
		if in := f.TokensIn(tok.Span()); tok.Span().End > tok.Span().Start && (len(in) != 1 || in[0] != tok) {
			t.Errorf("TokensIn(%v) = %v", tok.Span(), in)
		}
	}
}
