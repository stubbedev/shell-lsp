package dialect

import "slices"

func Choose[T any](scripts []T, detection func(T) Detection, host string) (Detection, []T) {
	dialects := make([]Dialect, len(scripts))
	for i, s := range scripts {
		dialects[i] = detection(s).Dialect
	}
	shared, fits := Shared(dialects)
	kept := slices.DeleteFunc(slices.Clone(scripts), func(s T) bool { return !fits(detection(s).Dialect) })
	i := slices.IndexFunc(kept, func(s T) bool { return detection(s).Dialect == shared })
	if i < 0 {
		return Detection{Dialect: shared, Source: SourceHostDefault, Evidence: host}, nil
	}
	return detection(kept[i]), kept
}
