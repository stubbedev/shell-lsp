package manpage

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/getopt"
)

type Listing struct {
	Subcommands []Subcommand
	Unloaded    []string
}

type availability uint8

const (
	absent availability = iota
	pending
	present
)

type resolver struct {
	lookup   *Lookup
	read     func(name string) (Page, error)
	unloaded []string
}

func (l *Lookup) loading(ctx context.Context) resolver {
	return resolver{lookup: l, read: func(name string) (Page, error) { return l.Page(ctx, name) }}
}

func (r *resolver) page(name string) (Page, availability, error) {
	if !r.lookup.Has(name) {
		return Page{}, absent, nil
	}
	hit, ok := r.fetch(name)
	if !ok {
		if !slices.Contains(r.unloaded, name) {
			r.unloaded = append(r.unloaded, name)
		}
		return Page{}, pending, nil
	}
	switch {
	case errors.Is(hit.err, ErrNoPage):
		return Page{}, absent, nil
	case hit.err != nil:
		return Page{}, absent, hit.err
	}
	return hit.page, present, nil
}

func (r *resolver) fetch(name string) (cached, bool) {
	if r.read == nil {
		return r.lookup.cached(name)
	}
	page, err := r.read(name)
	return cached{page: page, err: err}, true
}

type level struct {
	page  string
	path  []string
	node  Subcommand
	state availability
}

func (r *resolver) root(command string) (level, error) {
	page, state, err := r.page(command)
	return level{
		page:  command,
		path:  []string{command},
		node:  Subcommand{Name: command, Options: page.options, Subcommands: page.subcommands},
		state: state,
	}, err
}

func (r *resolver) child(parent *level, word string) (level, bool, error) {
	if !bareWord.MatchString(word) {
		return level{}, false, nil
	}
	listed, isListed := findSubcommand(parent.node.Subcommands, word)
	next := level{page: parent.page + "-" + word, path: append(slices.Clip(parent.path), word)}
	page, state, err := r.page(next.page)
	if err != nil {
		return level{}, false, err
	}
	next.state = state
	switch {
	case state == present && (isListed || page.invokes(parent.path, word)):
		next.node = fromPage(word, page, listed, parent.node)
	case state == pending || isListed:
		next.node = listed
		next.node.Name = word
	default:
		return level{}, false, nil
	}
	return next, true, nil
}

type nextWord uint8

const (
	nextSubcommand nextWord = iota
	nextValue
	nextOperand
)

type walk struct {
	level level
	named int
	next  nextWord
}

func (r *resolver) walk(command string, args []string) (walk, error) {
	root, err := r.root(command)
	if err != nil {
		return walk{}, err
	}
	w := walk{level: root, named: -1}
	for i := 0; i < len(args); {
		word := args[i]
		switch {
		case word == getopt.EndOfOptions:
			w.next = nextOperand
			return w, nil
		case getopt.IsOption(word):
			words, err := r.optionWords(&w.level, word, args[i+1:])
			if err != nil {
				return walk{}, err
			}
			if i += words; i > len(args) {
				w.next = nextValue
				return w, nil
			}
		default:
			child, ok, err := r.child(&w.level, word)
			if err != nil {
				return walk{}, err
			}
			if !ok {
				w.next = nextOperand
				return w, nil
			}
			w.level, w.named = child, i
			i++
		}
	}
	return w, nil
}

func (r *resolver) optionWords(lv *level, option string, rest []string) (int, error) {
	if words, known := getopt.Words(option, lv.node.arity); known {
		return words, nil
	}
	for skipped, word := range rest[:min(len(rest), 2)] {
		if _, ok, err := r.child(lv, word); err != nil || ok {
			return 1 + skipped, err
		}
	}
	return 1, nil
}

func (r *resolver) listing(lv *level) ([]Subcommand, error) {
	l := r.lookup
	l.mu.Lock()
	memo, ok := l.listings[lv.page]
	l.mu.Unlock()
	if ok {
		return memo, nil
	}
	subs := slices.Clone(lv.node.Subcommands)
	prefix := lv.page + "-"
	for _, pageName := range l.library.prefixed(prefix) {
		name := pageName[len(prefix):]
		if _, listed := findSubcommand(lv.node.Subcommands, name); listed || !bareWord.MatchString(name) {
			continue
		}
		page, state, err := r.page(pageName)
		if err != nil {
			return nil, err
		}
		if state == present && page.invokes(lv.path, name) {
			subs = append(subs, fromPage(name, page, Subcommand{}, lv.node))
		}
	}
	slices.SortFunc(subs, bySubcommandName)
	if len(r.unloaded) == 0 {
		l.mu.Lock()
		l.listings[lv.page] = subs
		l.mu.Unlock()
	}
	return subs, nil
}

func (l *Lookup) Subcommands(ctx context.Context, command string, args []string) (Listing, error) {
	if err := ctx.Err(); err != nil {
		return Listing{}, err
	}
	r := resolver{lookup: l}
	w, err := r.walk(command, args)
	if err != nil {
		return Listing{}, fmt.Errorf("resolving the subcommands of %s: %w", invocationText(command, args), err)
	}
	if w.next != nextSubcommand || w.level.state == pending {
		return Listing{Unloaded: r.unloaded}, nil
	}
	subs, err := r.listing(&w.level)
	if err != nil {
		return Listing{}, fmt.Errorf("listing the subcommands of %s: %w", invocationText(command, args), err)
	}
	return Listing{Subcommands: subs, Unloaded: r.unloaded}, nil
}

func (l *Lookup) Subcommand(ctx context.Context, command string, args []string) (Subcommand, error) {
	r := l.loading(ctx)
	w, err := r.walk(command, args)
	if err != nil {
		return Subcommand{}, fmt.Errorf("resolving %s: %w", invocationText(command, args), err)
	}
	if len(args) == 0 || w.named != len(args)-1 {
		return Subcommand{}, fmt.Errorf("%w: %s does not end in a documented subcommand", ErrNoPage, invocationText(command, args))
	}
	return w.level.node, nil
}

func (l *Lookup) Options(ctx context.Context, command string, args []string) ([]Option, error) {
	r := l.loading(ctx)
	w, err := r.walk(command, args)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", invocationText(command, args), err)
	}
	if w.named >= 0 {
		return w.level.node.Options, nil
	}
	page, err := l.Page(ctx, command)
	if err != nil {
		return nil, err
	}
	return page.options, nil
}

func (l *Lookup) Unloaded(command string, args []string) ([]string, error) {
	r := resolver{lookup: l}
	if _, err := r.walk(command, args); err != nil {
		return nil, fmt.Errorf("resolving %s: %w", invocationText(command, args), err)
	}
	return r.unloaded, nil
}

func invocationText(command string, args []string) string {
	return strings.Join(append([]string{command}, args...), " ")
}
