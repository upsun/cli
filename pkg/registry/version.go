package registry

import (
	"strconv"
	"strings"
)

// CompareVersions compares dotted version strings, numerically where both
// parts are numbers, returning a negative number if a < b, positive if a > b,
// or 0 if they are equal.
func CompareVersions(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		na, ea := strconv.Atoi(pa[i])
		nb, eb := strconv.Atoi(pb[i])
		if ea == nil && eb == nil {
			if na != nb {
				return na - nb
			}
			continue
		}
		if c := strings.Compare(pa[i], pb[i]); c != 0 {
			return c
		}
	}
	return len(pa) - len(pb)
}
