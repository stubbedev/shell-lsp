package justfile

import (
	"cmp"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const Version = "1.58.0"

type version [3]uint64

var versionPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$`)

var currentVersion = func() version {
	v, err := parseVersion(Version)
	if err != nil {
		panic(fmt.Sprintf("the implemented just version %q: %v", Version, err))
	}
	return v
}()

func parseVersion(text string) (version, error) {
	m := versionPattern.FindStringSubmatch(text)
	if m == nil {
		return version{}, errors.New("expected `MAJOR.MINOR.PATCH`")
	}
	var v version
	for i, part := range m[1:] {
		n, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return version{}, fmt.Errorf("version component %q: %w", part, err)
		}
		v[i] = n
	}
	return v, nil
}

func (v version) String() string {
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func (v version) compare(w version) int {
	return cmp.Or(cmp.Compare(v[0], w[0]), cmp.Compare(v[1], w[1]), cmp.Compare(v[2], w[2]))
}

func supportedVersion(text string) error {
	minimum, err := parseVersion(text)
	if err != nil {
		return fmt.Errorf("`minimum-version` setting has invalid version `%s`: %w", text, err)
	}
	if currentVersion.compare(minimum) < 0 {
		return fmt.Errorf("justfile requires just %s or later, but using %s", minimum, currentVersion)
	}
	return nil
}

func validIndentation(text string) error {
	if text == "" {
		return errors.New("the indentation must not be empty")
	}
	first, _ := utf8.DecodeRuneInString(text)
	if first != ' ' && first != '\t' {
		return errors.New("the indentation must be spaces or tabs")
	}
	if strings.Trim(text, string(first)) != "" {
		return errors.New("the indentation must be all spaces or all tabs")
	}
	return nil
}
