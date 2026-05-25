package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/clip"
)

// TestAuditFallbackPath enumerates which tests trigger the legacy
// per-edge fallback path (when [clip.BuildLocalMinima] fails). This is
// a diagnostic test for the §11/§12 audit — it just runs every public
// boolean op on a sampling of test inputs and reports.
func TestAuditFallbackPath(t *testing.T) {
	clip.SetFallbackTraceEnabled(true)
	defer func() {
		clip.SetFallbackTraceEnabled(false)
		clip.ClearFallbackTrace()
	}()

	cases := []struct {
		name string
		a, b MultiPolygon
	}{
		{"disjoint", MultiPolygon{sq(0, 0, 5)}, MultiPolygon{sq(20, 0, 5)}},
		{"touchingBoundary", MultiPolygon{sq(0, 0, 5)}, MultiPolygon{sq(10, 0, 5)}},
		{"overlapping", MultiPolygon{sq(0, 0, 5)}, MultiPolygon{sq(3, 0, 5)}},
		{"nested", MultiPolygon{sq(0, 0, 10)}, MultiPolygon{sq(0, 0, 3)}},
		{"overlappingDiamonds", MultiPolygon{diamond(0, 0, 10)}, MultiPolygon{diamond(5, 0, 10)}},
		{"touchingAtVertex", MultiPolygon{sq(0, 0, 5)}, MultiPolygon{sq(10, 10, 5)}},
	}
	ops := []struct {
		name string
		fn   func(a, b MultiPolygon) (MultiPolygon, error)
	}{
		{opUnion, Union},
		{opIntersect, Intersect},
		{opDifference, Difference},
		{opXor, Xor},
	}
	fellBack := false
	for _, c := range cases {
		for _, op := range ops {
			clip.ClearFallbackTrace()
			_, _ = op.fn(c.a, c.b)
			trace := clip.FallbackTrace()
			if len(trace) > 0 {
				fellBack = true
				t.Logf("%s/%s: FELL BACK: %v", c.name, op.name, trace[0])
			}
		}
	}
	if !fellBack {
		t.Log("AUDIT RESULT: bound model handles every audited input; legacy fallback is not exercised.")
	}
}
