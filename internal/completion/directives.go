package completion

import (
	"slices"
	"strconv"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/dialect"
	"github.com/stubbedev/shell-lsp/internal/directive"
	"github.com/stubbedev/shell-lsp/internal/hover"
	"github.com/stubbedev/shell-lsp/internal/lint"
	"github.com/stubbedev/shell-lsp/internal/parser"
)

const (
	directiveTriggers = "=,"
	allChecks         = "all"
	scriptDirectory   = "SCRIPTDIR"
)

var directiveKeyDocs = map[string]string{
	directive.Disable:          "Silence the listed checks for the next command, or for the whole file when the directive comes before the first command. Takes codes such as `SC2086`, ranges such as `SC2000-SC2099`, or `all`, separated by commas.",
	directive.Enable:           "Turn on optional checks by name, or every optional check with `all`. Only honoured before the first command.",
	directive.Source:           "The file the following `source` or `.` command loads, for when its path is computed at run time. `/dev/null` skips it.",
	directive.SourcePath:       "A directory to look for sourced files in. `SCRIPTDIR` stands for the directory of the script itself.",
	directive.Shell:            "Lint the file as this shell, whatever its shebang says.",
	directive.ExternalSources:  "Whether sourced files outside the ones being checked may be followed. Only honoured in a `.shellcheckrc` file.",
	directive.ExtendedAnalysis: "Turn the checks that follow values through the script on (`true`) or off (`false`) for this file.",
}

func directiveTrigger(trigger string) bool {
	return trigger != "" && strings.Contains(directiveTriggers, trigger)
}

func directiveCompletion(in Input) (Result, bool, error) {
	text := in.Tree.Text()
	offset := min(max(in.Offset, 0), len(text))
	for _, comment := range in.Tree.Comments {
		if comment.Span.Start >= offset || offset > comment.Span.End {
			continue
		}
		cursor, ok := directive.CursorAt(text[comment.Span.Start:comment.Span.End], offset-comment.Span.Start)
		if !ok {
			return Result{}, false, nil
		}
		c := completer{Input: in, dialect: in.Tree.Dialect}
		start, end := comment.Span.Start+cursor.Start, comment.Span.Start+cursor.End
		if cursor.Slot == directive.SlotValue && directive.ListKey(cursor.Key) {
			if dash := strings.LastIndexByte(text[start:offset], '-'); dash >= 0 {
				start += dash + 1
			}
		}
		c.site = site{prefix: text[start:offset], replace: parser.Span{Start: start, End: offset}, wordEnd: end}
		result, err := c.directiveItems(cursor, text[end:])
		return result, true, err
	}
	return Result{}, false, nil
}

func (c completer) directiveItems(cursor directive.Cursor, after string) (Result, error) {
	if cursor.Slot == directive.SlotKey {
		return Result{Items: c.directiveKeys(strings.HasPrefix(after, "="))}, nil
	}
	switch cursor.Key {
	case directive.Disable:
		return Result{Items: c.checkCodes(cursor.Others)}, nil
	case directive.Enable:
		return Result{Items: c.optionalChecks(cursor.Others)}, nil
	case directive.Shell:
		return Result{Items: c.shellNames()}, nil
	case directive.ExternalSources, directive.ExtendedAnalysis:
		return Result{Items: c.choices("true", "false")}, nil
	case directive.Source:
		return c.paths(false)
	case directive.SourcePath:
		paths, err := c.paths(true)
		paths.Items = append(c.choices(scriptDirectory), paths.Items...)
		return paths, err
	}
	return Result{}, nil
}

func (c completer) directiveKeys(hasEquals bool) []Item {
	var items []Item
	for i, key := range directive.Keys() {
		if !strings.HasPrefix(key, c.site.prefix) {
			continue
		}
		it := c.item(key, KindKey, strconv.Itoa(i))
		if !hasEquals {
			it.InsertText = key + "="
		}
		it.Documentation = directiveKeyDocs[key]
		items = append(items, it)
	}
	return items
}

func (c completer) checkCodes(listed []string) []Item {
	typed := strings.ToUpper(c.site.prefix)
	digits := typed != "" && parser.IsDigits(typed)
	var items []Item
	if !slices.Contains(listed, allChecks) && strings.HasPrefix(allChecks, c.site.prefix) {
		it := c.item(allChecks, KindValue, "0")
		it.Documentation = "Every check."
		items = append(items, it)
	}
	for _, info := range lint.Catalog() {
		label := info.Code.String()
		number := label[2:]
		matches := strings.HasPrefix(label, typed) || digits && strings.HasPrefix(number, typed)
		if !matches || listedCode(listed, info.Code) {
			continue
		}
		it := c.item(label, KindValue, "1")
		if digits {
			it.FilterText = number
		}
		it.Detail = info.Severity.String()
		it.LabelDetail = codeLabelDetail(info)
		it.Documentation = hover.CodeDoc(info)
		items = append(items, it)
	}
	return items
}

func listedCode(listed []string, code lint.Code) bool {
	return slices.ContainsFunc(listed, func(item string) bool {
		parsed, err := lint.ParseCode(item)
		return err == nil && parsed == code
	})
}

func codeLabelDetail(info lint.CodeInfo) string {
	switch {
	case info.Summary != "":
		return " " + info.Summary
	case info.Optional != "":
		return " optional: " + info.Optional
	}
	return ""
}

func (c completer) optionalChecks(listed []string) []Item {
	var items []Item
	for i, name := range append([]string{allChecks}, lint.OptionalChecks()...) {
		if slices.Contains(listed, name) || !strings.HasPrefix(name, c.site.prefix) {
			continue
		}
		sort := "1"
		if i == 0 {
			sort = "0"
		}
		it := c.item(name, KindOption, sort)
		it.Documentation = optionalCheckDoc(name)
		items = append(items, it)
	}
	return items
}

func optionalCheckDoc(name string) string {
	if name == allChecks {
		return "Every optional check."
	}
	var codes []string
	for _, info := range lint.Catalog() {
		if info.Optional == name {
			codes = append(codes, "`"+info.Code.String()+"`")
		}
	}
	if len(codes) == 0 {
		return "Changes how other checks behave rather than adding findings of its own."
	}
	return "Turns on " + strings.Join(codes, ", ") + "."
}

func (c completer) shellNames() []Item {
	var items []Item
	for d := range dialect.All.Members() {
		name := d.String()
		if !strings.HasPrefix(name, c.site.prefix) {
			continue
		}
		it := c.item(name, KindValue, "1")
		it.Documentation = "Lint this file as " + name + "."
		items = append(items, it)
	}
	return items
}

func (c completer) choices(values ...string) []Item {
	var items []Item
	for i, value := range values {
		if strings.HasPrefix(value, c.site.prefix) {
			items = append(items, c.item(value, KindValue, strconv.Itoa(i)))
		}
	}
	return items
}
