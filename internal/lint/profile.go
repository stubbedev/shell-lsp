package lint

import "slices"

type Profile uint8

const (
	ProfileRecommended Profile = iota
	ProfileMinimal
	ProfileStrict
)

var profileNames = [...]string{ProfileRecommended: "recommended", ProfileMinimal: "minimal", ProfileStrict: "strict"}

func (p Profile) String() string {
	if int(p) < len(profileNames) {
		return profileNames[p]
	}
	return "unknown"
}

func ProfileNamed(name string) (Profile, bool) {
	for profile, n := range profileNames {
		if n == name {
			return Profile(profile), true
		}
	}
	return 0, false
}

func ProfileNames() []string { return slices.Clone(profileNames[:]) }

var strictSkips = []string{uppercaseOptional}

func StrictSkips() []string { return slices.Clone(strictSkips) }

func (p Profile) enables(optional string) bool {
	return p == ProfileStrict && !slices.Contains(strictSkips, optional)
}

func (p Profile) keeps(f Finding, m meta, chosen bool) bool {
	return p != ProfileMinimal || chosen || minimalKeeps(f.Severity, m.bug)
}

func minimalKeeps(severity Severity, bug bool) bool {
	return severity <= SeverityWarning || severity == SeverityInfo && bug
}
