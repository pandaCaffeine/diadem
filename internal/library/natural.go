package library

import "strings"

// NaturalLess orders strings the way humans read them: embedded digit runs
// compare numerically, so "S02E9" sorts before "S02E10". Comparison is
// case-insensitive; digits sort before letters; numerically equal runs
// ("a01" vs "a1") compare equal.
func NaturalLess(a, b string) bool {
	a = strings.ToLower(a)
	b = strings.ToLower(b)
	for len(a) > 0 && len(b) > 0 {
		da, db := isDigit(a[0]), isDigit(b[0])
		switch {
		case da && db:
			// Compare whole digit runs numerically, then continue with
			// whatever follows each run.
			ia, ib := 0, 0
			for ia < len(a) && isDigit(a[ia]) {
				ia++
			}
			for ib < len(b) && isDigit(b[ib]) {
				ib++
			}
			na := strings.TrimLeft(a[:ia], "0")
			nb := strings.TrimLeft(b[:ib], "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			a, b = a[ia:], b[ib:]
		case da != db:
			return da // digits before letters
		case a[0] != b[0]:
			return a[0] < b[0]
		default:
			a, b = a[1:], b[1:]
		}
	}
	// Whatever ended first is the prefix, and sorts first.
	return len(a) < len(b)
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }
