package lint

import "slices"

type Profile uint8

const (
	ProfileRecommended Profile = iota
	ProfileMinimal
	ProfileStrict
)

var profileNames = nameTable[Profile]{ProfileRecommended: "recommended", ProfileMinimal: "minimal", ProfileStrict: "strict"}

func (p Profile) String() string { return profileNames.name(p) }

func ProfileNamed(name string) (Profile, bool) { return profileNames.named(name) }

func ProfileNames() []string { return slices.Clone(profileNames) }

func (p Profile) enables(optional optionalCheck) bool {
	return p == ProfileStrict && !optionalSpecs[optional].notStrict
}

func (p Profile) keeps(f Finding, m meta, chosen bool) bool {
	return p != ProfileMinimal || chosen || minimalKeeps(f.Severity, m.bug)
}

func minimalKeeps(severity Severity, bug bool) bool {
	return severity <= SeverityWarning || severity == SeverityInfo && bug
}
