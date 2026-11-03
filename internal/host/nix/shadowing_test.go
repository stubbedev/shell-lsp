package nix

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/stubbedev/shell-lsp/internal/host/model"
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

func TestTheLibraryKnowsWhatItsFixerAdds(t *testing.T) {
	t.Parallel()

	entry := "let\n  makeExtensible' = rattrs: let self = rattrs self // {\n    # Patches the library.\n    extend = f: f;\n  }; in self;\n  lib = makeExtensible' (self: { a = 1; extend = 2; });\nin lib\n"
	root := filepath.FromSlash("/np/lib/default.nix")
	read := func(path string) (string, bool) { return entry, path == root }
	library := NewLibrary(filepath.FromSlash("/np"), read)
	got, found := library.Lookup([]string{"extend"})
	if !found {
		t.Fatal("lib.extend was not found")
	}
	if got.Doc != "Patches the library." || got.Declaration.Line != 4 {
		t.Errorf("lib.extend = %+v, want the fixer's own attribute, which overrides the library's", got)
	}
	if _, found := NewLibrary(filepath.FromSlash("/np"), read).Lookup([]string{"a"}); !found {
		t.Error("lib.a is lost beside the fixer's attribute")
	}
	if members := NewLibrary(filepath.FromSlash("/np"), read).Members(nil); !slices.Contains(members, "extend") || !slices.Contains(members, "a") {
		t.Errorf("members = %q", members)
	}
}

func TestLibraryNamesAreResolvedInScope(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/np/lib/default.nix")
	for _, tt := range []struct {
		name, entry string
		names       []string
		builtin     string
		found       bool
	}{
		{"inherit from the global builtins", "let inherit (builtins) mapAttrs; in { inherit mapAttrs; }", []string{"mapAttrs"}, "mapAttrs", true},
		{"inherit from a local set named builtins", "let builtins = { mapAttrs = 1; }; inherit (builtins) mapAttrs; in { inherit mapAttrs; }", []string{"mapAttrs"}, "", true},
		{"inherit from a local set without the name", "let builtins = { }; inherit (builtins) mapAttrs; in { inherit mapAttrs; }", []string{"mapAttrs"}, "", false},
		{"the file's own formal is the library", "{ lib }: { top = 1; again = lib; }", []string{"again", "top"}, "", true},
		{"a formal of an inner function is not the library", "{ top = 1; again = { lib }: lib; }", []string{"again", "top"}, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			read := func(path string) (string, bool) { return tt.entry, path == root }
			got, found := NewLibrary(filepath.FromSlash("/np"), read).Lookup(tt.names)
			if found != tt.found || got.Builtin != tt.builtin {
				t.Errorf("Lookup(%q) = %+v, %v; want builtin %q, found %v", tt.names, got, found, tt.builtin, tt.found)
			}
		})
	}
}

func TestOnlyTheGlobalBuiltinsImport(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, src string
		follows   bool
	}{
		{"the global builtins", "builtins.import ./a.nix\n", true},
		{"a local set named builtins", "let builtins = { import = x: x; }; in builtins.import ./a.nix\n", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tree := syntax.Parse(tt.src)
			var call *syntax.Apply
			syntax.Inspect(tree.Root, func(n syntax.Node) bool {
				if apply, isCall := n.(*syntax.Apply); isCall {
					call = apply
				}
				return true
			})
			table := newSymbolTable(&file{src: tt.src, tree: tree, table: scope.Resolve(tree)}, &model.Index{})
			if got := table.builtinImport(call.Fn); got != tt.follows {
				t.Errorf("builtinImport of %q = %v, want %v", tt.src, got, tt.follows)
			}
		})
	}
}

func TestThePackageSetIsReadFromItsSource(t *testing.T) {
	t.Parallel()

	files := map[string]string{
		filepath.FromSlash("/np/pkgs/top-level/all-packages.nix"):                 "{ lib }:\nres: pkgs: super:\nwith pkgs;\n{\n  called = callPackage ../tools/called { };\n  python3 = { pkgs = { x = 1; }; };\n}\n",
		filepath.FromSlash("/np/pkgs/tools/called/default.nix"):                   "{ stdenv }:\nstdenv.mkDerivation { meta = { description = \"Called.\"; }; }\n",
		filepath.FromSlash("/np/pkgs/build-support/trivial-builders/default.nix"): "{ }:\nrec {\n  # Runs a command.\n  runCommand = name: name;\n}\n",
		filepath.FromSlash("/np/pkgs/by-name/he/hello/package.nix"):               "{ stdenv }:\nstdenv.mkDerivation { meta.description = \"Says hello.\"; }\n",
	}
	read := func(path string) (string, bool) {
		text, ok := files[path]
		return text, ok
	}
	for _, tt := range []struct {
		names []string
		file  string
		line  int
		doc   string
	}{
		{[]string{"called"}, "/np/pkgs/tools/called/default.nix", 1, "Called."},
		{[]string{"python3", "pkgs", "x"}, "/np/pkgs/top-level/all-packages.nix", 6, ""},
		{[]string{"runCommand"}, "/np/pkgs/build-support/trivial-builders/default.nix", 4, "Runs a command."},
		{[]string{"hello"}, "/np/pkgs/by-name/he/hello/package.nix", 1, "Says hello."},
	} {
		entry, ok := NewLibrary(filepath.FromSlash("/np"), read).Package(tt.names)
		if !ok {
			t.Errorf("pkgs.%v was not found", tt.names)
			continue
		}
		if entry.Declaration.File != filepath.FromSlash(tt.file) || entry.Declaration.Line != tt.line || entry.Doc != tt.doc {
			t.Errorf("pkgs.%v = %+v", tt.names, entry)
		}
	}
	for _, absent := range [][]string{{"nothing"}, {"hello", "dev"}, {"x"}} {
		if entry, ok := NewLibrary(filepath.FromSlash("/np"), read).Package(absent); ok {
			t.Errorf("pkgs.%v = %+v, want nothing", absent, entry)
		}
	}
}

func TestTheLibraryLooksThroughCallsThatKeepAttributes(t *testing.T) {
	t.Parallel()

	root := filepath.FromSlash("/np/lib/default.nix")
	entry := "{ licenses = mapAttrs mk { mit = 1; }; kept = recurseIntoAttrs { a = 1; }; lost = other { b = 1; }; }"
	read := func(path string) (string, bool) { return entry, path == root }
	for _, names := range [][]string{{"licenses", "mit"}, {"kept", "a"}} {
		if _, found := NewLibrary(filepath.FromSlash("/np"), read).Lookup(names); !found {
			t.Errorf("lib.%v was not found", names)
		}
	}
	if got, found := NewLibrary(filepath.FromSlash("/np"), read).Lookup([]string{"lost", "b"}); found {
		t.Errorf("lib.lost.b = %+v through a function that may change its attributes", got)
	}
}
