package nix

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/nix/scope"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
)

type recognition struct {
	name, src string
	real      bool
}

func TestAShadowedMkOptionDeclaresNoOptionType(t *testing.T) {
	t.Parallel()

	for _, tt := range []recognition{
		{"lib select", "{ lib }: lib.mkOption { type = t; }", true},
		{"a formal", "{ mkOption }: mkOption { type = t; }", true},
		{"an alias of the real one", "{ lib }: let mkOption = lib.mkOption; in mkOption { type = t; }", true},
		{"unbound", "mkOption { type = t; }", true},
		{"a local lambda", "let mkOption = x: x; in mkOption { type = t; }", false},
		{"a local set", "let lib = { mkOption = x: x; }; in lib.mkOption { type = t; }", false},
		{"a lambda given a function", "(mkOption: mkOption { type = t; }) (x: x)", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := syntax.Parse(tt.src)
			f := &file{src: tt.src, tree: tree, table: scope.Resolve(tree)}
			var call *syntax.Apply
			syntax.Inspect(tree.Root, func(n syntax.Node) bool {
				if apply, isCall := n.(*syntax.Apply); isCall {
					if _, literal := syntax.Unparenthesized(apply.Args[0]).(*syntax.AttrSet); literal && call == nil {
						call = apply
					}
				}
				return true
			})
			if _, typed := optionType(&symbolTable{f: f}, flowSource{expr: call}); typed != tt.real {
				t.Errorf("optionType recognised = %v, want %v", typed, tt.real)
			}
		})
	}
}

func TestAShadowedCallPackageLoadsNoPath(t *testing.T) {
	t.Parallel()

	for _, tt := range []recognition{
		{"a package formal", "{ lib, callPackage, ... }: callPackage ./a.nix { }", true},
		{"pkgs select", "{ pkgs }: pkgs.callPackage ./a.nix { }", true},
		{"a lambda given the real one", "(callPackage: callPackage ./a.nix { }) pkgs.callPackage", true},
		{"a pattern given the real one", "({ callPackage }: callPackage ./a.nix { }) { inherit callPackage; }", true},
		{"a local lambda", "let callPackage = x: y: y; in callPackage ./a.nix { }", false},
		{"a lambda given a function", "(callPackage: callPackage ./a.nix { }) (x: y: y)", false},
		{"a pattern given a function", "({ callPackage }: callPackage ./a.nix { }) { callPackage = x: y: y; }", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			literals := Read(tt.src).PathLiterals()
			if len(literals) != 1 || literals[0].Loaded != tt.real {
				t.Errorf("literals = %+v, want one with Loaded %v", literals, tt.real)
			}
		})
	}
}

func TestAShadowedMkFlakeDeclaresNoOutputs(t *testing.T) {
	t.Parallel()

	for _, tt := range []recognition{
		{"a formal", "{ outputs = { mkFlake, ... }: mkFlake { } { perSystem.apps.a = 1; }; }", true},
		{"flake-parts", "{ outputs = inputs: inputs.parts.lib.mkFlake { } { perSystem.apps.a = 1; }; }", true},
		{"a local lambda", "{ outputs = _: let mkFlake = a: b: { }; in mkFlake { } { perSystem.apps.a = 1; }; }", false},
		{"a local each-system", "{ outputs = _: let eachDefaultSystem = f: { }; in eachDefaultSystem (s: { apps.a = 1; }); }", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := len(outputNames(tt.src)) > 0; got != tt.real {
				t.Errorf("outputs = %q, want recognised %v", outputNames(tt.src), tt.real)
			}
		})
	}
}

func TestAShadowedWrapperEndsTheKeysOfAMember(t *testing.T) {
	t.Parallel()

	for _, tt := range []recognition{
		{"lib select", "{ lib }: { a = lib.mkIf true { b| = 1; }; }", true},
		{"a formal", "{ mkIf }: { a = mkIf true { b| = 1; }; }", true},
		{"a local lambda", "let mkIf = c: v: v; in { a = mkIf true { b| = 1; }; }", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			cursor, located := Locate(text, at, NoFiles)
			if !located {
				t.Fatalf("no cursor in %q", tt.src)
			}
			request, wanted := cursor.MembersWanted()
			if !wanted {
				t.Fatalf("no members wanted in %q", tt.src)
			}
			if crossed := len(request.sets) > 1; crossed != tt.real {
				t.Errorf("key candidates = %+v, want the outer set %v", request.sets, tt.real)
			}
		})
	}
}

func TestAShadowedFunctionIsNoOptionSite(t *testing.T) {
	t.Parallel()

	for _, tt := range []recognition{
		{"a formal mkIf", "{ mkIf, ... }: { config = mkIf true { services.| }; }", true},
		{"an alias of mkMerge", "{ lib, ... }: let mkMerge = lib.mkMerge; in { config = mkMerge [ { services.| } ]; }", true},
		{"a local mkIf", "let mkIf = c: v: { }; in { config = mkIf true { services.| }; }", false},
		{"a local mkMerge", "let mkMerge = x: { }; in { config = mkMerge [ { services.| } ]; }", false},
		{"a local mkFlake", "{ outputs = inputs: let mkFlake = a: b: { }; in mkFlake { } { perSystem.| }; }", false},
		{"a local nixosSystem", "let nixosSystem = x: { }; in { a = nixosSystem { modules = [ { services.| } ]; }; }", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			text, at := marked(t, tt.src)
			known := newFakeEvaluated()
			Complete(text, at, NoFiles, known)
			if got := len(known.sites) > 0; got != tt.real {
				t.Errorf("asked for sites %+v, want asked %v", known.sites, tt.real)
			}
		})
	}
}

func TestTheLibraryFollowsOnlyAFixedPoint(t *testing.T) {
	t.Parallel()

	const body = "(self: { a = 1; })"
	for _, tt := range []struct {
		name, entry string
		real        bool
	}{
		{"unbound", "makeExtensible " + body, true},
		{"inherited from the file that defines it", "let inherit (import ./fixed-points.nix { }) fix; in fix " + body, true},
		{"nixpkgs' own lib/default.nix", "let\n  makeExtensible' = rattrs: let self = rattrs self // { extend = 1; }; in self;\n  lib = makeExtensible' " + body + ";\nin lib\n", true},
		{"lib/fixed-points.nix's fix", "let fix = f: let x = f x; in x; in fix " + body, true},
		{"lib/fixed-points.nix's extends", "let extends = overlay: f: final: f final // overlay final; in extends { } " + body, true},
		{"a selected fixed point", "{ lib }: lib.makeExtensible " + body, true},
		{"a local identity", "let fix = x: x; in fix " + body, false},
		{"a local constant", "let makeExtensible = x: { }; in makeExtensible " + body, false},
		{"a function too short for its arguments", "let makeScope = x: { }; in makeScope { } " + body, false},
		{"a lambda given an identity", "(fix: fix " + body + ") (x: x)", false},
		{"a member of a local set", "let s = { fix = f: f { }; }; in s.fix " + body, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root, fixedPoints := filepath.FromSlash("/np/lib/default.nix"), filepath.FromSlash("/np/lib/fixed-points.nix")
			read := func(path string) (string, bool) {
				switch path {
				case root:
					return tt.entry, true
				case fixedPoints:
					return "{ }: { fix = f: let x = f x; in x; }", true
				}
				return "", false
			}
			if _, found := NewLibrary(filepath.FromSlash("/np"), read).Lookup([]string{"a"}); found != tt.real {
				t.Errorf("lib.a found = %v, want %v", found, tt.real)
			}
		})
	}
}

func TestSuppliedValuesComeOnlyFromADirectCall(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		want      []string
	}{
		{"a named parameter", "(a: a) 1", []string{"a"}},
		{"a pattern", "({ a, b ? 2, c }@all: a) { a = 1; b = 3; }", []string{"a", "all", "b"}},
		{"a lambda never called", "a: a", nil},
		{"a default alone", "{ a ? 1 }: a", nil},
		{"a lambda called through a name", "let f = a: a; in f 1", nil},
		{"an inherited argument", "({ a }: a) { inherit a; }", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			table := scope.Resolve(syntax.Parse(tt.src))
			var got []string
			for _, d := range table.Defs() {
				if _, supplied := table.Supplied(d); supplied {
					got = append(got, d.Name)
				}
			}
			slices.Sort(got)
			if !slices.Equal(got, tt.want) {
				t.Errorf("supplied = %q, want %q", got, tt.want)
			}
		})
	}
}
