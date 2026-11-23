package dockerfile

import "testing"

func TestContextNameIsTheFamiliarReference(t *testing.T) {
	t.Parallel()

	digest := "sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	for _, tt := range []struct{ ref, want string }{
		{"infra", "infra"},
		{"infra:latest", "infra"},
		{"infra:1.2", "infra:1.2"},
		{"library/alpine", "alpine"},
		{"docker.io/library/alpine:3", "alpine:3"},
		{"index.docker.io/alpine", "alpine"},
		{"docker.io/org/app", "org/app"},
		{"library/a/b", "library/a/b"},
		{"localhost:5000/library/infra", "localhost:5000/library/infra"},
		{"ghcr.io/org/app:latest", "ghcr.io/org/app"},
		{"app:latest@" + digest, "app:latest@" + digest},
		{"app@" + digest, "app@" + digest},
	} {
		parsed, ok := ParseReference(tt.ref)
		if !ok {
			t.Fatalf("ParseReference(%q) failed", tt.ref)
		}
		if got := parsed.ContextName(); got != tt.want {
			t.Errorf("ContextName(%q) = %q, want %q", tt.ref, got, tt.want)
		}
	}
}
