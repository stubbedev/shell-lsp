package hosttest

import (
	"strings"
)

type chooser struct {
	choices []byte
	at      int
}

func (c *chooser) next(n int) int {
	if c.at >= len(c.choices) {
		return 0
	}
	b := c.choices[c.at]
	c.at++
	return int(b) % n
}

func (c *chooser) pick(options ...string) string { return options[c.next(len(options))] }

func (c *chooser) done() bool { return c.at >= len(c.choices) }

var (
	genVariables = []string{"CC", "CFLAGS", "LDFLAGS", "SRCS", "OBJS", "PREFIX", "DESTDIR", "BUILD_DIR", "LIBS", "VERSION", "TARGET", "DEPS"}
	genTargets   = []string{"all", "clean", "install", "app", "test", "docs", "dist", "lib.a"}
	genFiles     = []string{"main.c", "util.c", "src/io.c", "README.md", "config.h"}
	genFunctions = []string{"patsubst %.c,%.o,", "addprefix $(BUILD_DIR)/,", "addsuffix .o,", "filter %.c,", "filter-out main.c,", "sort ", "strip ", "notdir ", "dir ", "basename ", "words ", "firstword ", "wildcard ", "subst .c,.o,"}
)

func GenerateMakefile(choices []byte) string {
	c := &chooser{choices: choices}
	var sb strings.Builder
	for lines := 0; lines < 40 && (lines == 0 || !c.done()); lines++ {
		genStatement(c, &sb)
	}
	return sb.String()
}

func genStatement(c *chooser, sb *strings.Builder) {
	switch c.next(12) {
	case 0, 1, 2:
		genAssignment(c, sb)
	case 3, 4, 5:
		genRule(c, sb)
	case 6:
		sb.WriteString(".PHONY: " + genWords(c, genTargets, 1+c.next(3)) + "\n")
	case 7:
		sb.WriteString(c.pick("# build settings", "# targets", "## help: build it") + "\n")
	case 8:
		sb.WriteString("\n")
	case 9:
		genDefine(c, sb)
	case 10:
		sb.WriteString(c.pick("export ", "unexport ") + c.pick(genVariables...) + "\n")
	case 11:
		genMistake(c, sb)
	}
}

func genWords(c *chooser, pool []string, n int) string {
	words := make([]string, n)
	for i := range words {
		words[i] = c.pick(pool...)
	}
	return strings.Join(words, " ")
}

func genReference(c *chooser) string {
	name := c.pick(genVariables...)
	switch c.next(6) {
	case 0:
		return "${" + name + "}"
	case 1:
		return "$(" + c.pick(genFunctions...) + "$(" + name + "))"
	case 2:
		return "$(" + name + ":.c=.o)"
	case 3:
		return "$(foreach f,$(" + name + "),$(f).o)"
	case 4:
		return "$(if $(" + name + "),yes,no)"
	}
	return "$(" + name + ")"
}

func genValue(c *chooser) string {
	parts := make([]string, 1+c.next(3))
	for i := range parts {
		switch c.next(4) {
		case 0:
			parts[i] = genReference(c)
		case 1:
			parts[i] = c.pick(genFiles...)
		case 2:
			parts[i] = c.pick("-O2", "-Wall", "-g", "-lm", "/usr/local", "1.0", "build", "gcc")
		case 3:
			parts[i] = ""
		}
	}
	value := strings.TrimSpace(strings.Join(parts, " "))
	if c.next(8) == 0 {
		value += " \\\n\t" + c.pick(genFiles...)
	}
	return value
}

func genAssignment(c *chooser, sb *strings.Builder) {
	name := c.pick(genVariables...)
	op := c.pick("=", ":=", "::=", "?=", "+=")
	space := c.pick(" ", "")
	if c.next(5) == 0 {
		sb.WriteString(c.pick(genTargets...) + ": ")
	}
	sb.WriteString(name + space + op + space + genValue(c) + "\n")
}

func genRule(c *chooser, sb *strings.Builder) {
	target := c.pick(genTargets...)
	colon := c.pick(":", ":", ":", "::")
	switch c.next(6) {
	case 0:
		sb.WriteString("%.o" + colon + " %.c")
	case 1:
		sb.WriteString("$(OBJS): %.o: %.c")
	default:
		sb.WriteString(target + colon)
		if n := c.next(4); n > 0 {
			sb.WriteString(" " + genWords(c, append(append([]string{}, genTargets...), genFiles...), n))
		}
		if c.next(6) == 0 {
			sb.WriteString(" | " + c.pick("build", "$(BUILD_DIR)"))
		}
	}
	if c.next(5) == 0 {
		sb.WriteString(" ; @echo " + genReference(c) + "\n")
		return
	}
	sb.WriteString("\n")
	for range c.next(4) {
		sb.WriteString("\t" + c.pick("@", "-", "", "") + c.pick(
			"$(CC) $(CFLAGS) -o $@ $^",
			"$(CC) -c $< -o $@",
			"echo "+genReference(c),
			"rm -f $(OBJS) $(TARGET)",
			"mkdir -p $(@D)",
			"install -m 755 $(TARGET) $(DESTDIR)$(PREFIX)/bin",
			"for f in $^; do echo $$f; done",
		) + "\n")
	}
}

func genDefine(c *chooser, sb *strings.Builder) {
	sb.WriteString("define " + c.pick("BANNER", "COMPILE", "HELP") + c.pick("", " =", " :=") + "\n")
	for range 1 + c.next(3) {
		sb.WriteString(c.pick("@echo building "+genReference(c), "$(CC) $(CFLAGS) -c $<", "") + "\n")
	}
	sb.WriteString("endef\n")
}

func genMistake(c *chooser, sb *strings.Builder) {
	name := c.pick(genVariables...)
	switch c.next(9) {
	case 0:
		sb.WriteString(name + " = $(" + name + " -O2\n")
	case 1:
		sb.WriteString(name + " = $(" + name + ") -O2\n")
	case 2:
		sb.WriteString(name + " := $(wild *.c)\n")
	case 3:
		sb.WriteString(name + " := $(subst .c,.o)\n")
	case 4:
		sb.WriteString(c.pick(genTargets...) + ":\n    echo indented with spaces\n")
	case 5:
		sb.WriteString(c.pick(genTargets...) + "\n")
	case 6:
		t := c.pick(genTargets...)
		sb.WriteString(t + ":\n\t@echo one\n" + t + ":\n\t@echo two\n")
	case 7:
		sb.WriteString("\techo before any rule\n")
	case 8:
		sb.WriteString(c.pick(genTargets...) + ": ; @echo ${" + name + "\n")
	}
}
