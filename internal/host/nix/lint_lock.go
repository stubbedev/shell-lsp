package nix

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/stubbedev/shell-lsp/internal/diskwatch"
	"github.com/stubbedev/shell-lsp/internal/filecache"
	"github.com/stubbedev/shell-lsp/internal/host/nix/syntax"
	"github.com/stubbedev/shell-lsp/internal/lint"
)

type LockFiles struct {
	files *filecache.Cache[flakeLock]
}

func NewLockFiles(read filecache.ReadFile, now func() time.Time, recheck time.Duration) *LockFiles {
	parse := func(path string, data []byte) (flakeLock, error) {
		lock, ok := parseFlakeLock(data)
		if !ok {
			return flakeLock{}, fmt.Errorf("%s is not a flake lock file", path)
		}
		return lock, nil
	}
	return &LockFiles{files: filecache.New(read, parse, now, recheck)}
}

func (l *LockFiles) Watch(disk *diskwatch.Watcher) { l.files.Watch(disk) }

func (l *LockFiles) Invalidate(path string) { l.files.Invalidate(path) }

func (l *LockFiles) FindingsFor(path, text string, enabled lint.CodeSet) ([]lint.Finding, error) {
	if filepath.Base(path) != FlakeFile {
		return nil, nil
	}
	tree := syntax.Parse(text)
	if len(tree.Errors) > 0 || !flakeShaped(tree.Root) {
		return nil, nil
	}
	lock, found, err := l.files.Load(filepath.Join(filepath.Dir(path), LockFileName))
	if err != nil || !found {
		return nil, err
	}
	kept := unsuppressed(tree, lockFindings(tree, lock, enabled))
	lint.SortHostFindings(kept)
	return kept, nil
}

func lockFindings(tree *syntax.File, lock flakeLock, enabled lint.CodeSet) []lint.Finding {
	var out []lint.Finding
	root := lock.Nodes[lock.Root]
	overridden := map[string]map[string]bool{}
	reported := map[syntax.Attr]bool{}
	eachInputLeaf(tree.Root, func(path syntax.AttrPath, _ syntax.Expr) {
		if len(path) < 3 {
			return
		}
		name, static := syntax.AttrName(path[0])
		via, viaStatic := syntax.AttrName(path[1])
		target, targetStatic := syntax.AttrName(path[2])
		if !static || !viaStatic || !targetStatic || via != inputsAttr {
			return
		}
		if overridden[name] == nil {
			overridden[name] = map[string]bool{}
		}
		overridden[name][target] = true
		reference, listed := root.Inputs[name]
		if !listed || reported[path[2]] {
			return
		}
		node, known := lock.nodeOf(reference, 0)
		if !known {
			return
		}
		if _, has := node.Inputs[target]; !has {
			reported[path[2]] = true
			out = append(out, lint.HostFinding(lint.CodeNixFollowsMissingInput, path[2].Span(), fmt.Sprintf("the locked `%s` has no input `%s`, so this override does nothing and Nix warns about it", name, target)))
		}
	})
	if !enabled.Has(lint.CodeNixMissingNixpkgsFollows) {
		return out
	}
	shared, sharedKnown := root.Inputs[mainInput]
	if !sharedKnown {
		return out
	}
	declared, _ := flakeInputsOf(tree.Root)
	for _, input := range declared {
		if input.name == mainInput || overridden[input.name][mainInput] {
			continue
		}
		reference, listed := root.Inputs[input.name]
		if !listed {
			continue
		}
		node, known := lock.nodeOf(reference, 0)
		if !known {
			continue
		}
		own, has := node.Inputs[mainInput]
		if !has || lock.sameNode(own, shared) {
			continue
		}
		out = append(out, lint.HostFinding(lint.CodeNixMissingNixpkgsFollows, input.attr.Span(), fmt.Sprintf("`%s` locks its own nixpkgs; `inputs.%s.inputs.nixpkgs.follows = \"nixpkgs\"` would share this flake's", input.name, input.name)))
	}
	return out
}

func (l flakeLock) sameNode(a, b json.RawMessage) bool {
	var left, right string
	if json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil {
		return left == right
	}
	first, firstKnown := l.nodeOf(a, 0)
	second, secondKnown := l.nodeOf(b, 0)
	return firstKnown && secondKnown && first.Locked.NarHash != "" && first.Locked.NarHash == second.Locked.NarHash
}
