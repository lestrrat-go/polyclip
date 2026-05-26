package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// TestSelfUnionPositiveRobustDegenerate guards the two ways the multi-frame
// self-union resolver used to abort the whole process on a degenerate input
// (both found by feeding selfUnionPositive two overlapping skyline polygons):
//
//   - an UNBOUNDED SPIN + OOM: a rotated frame collapses two distinct walls onto
//     one column, so reconcileSharedVertexCrossings never settles and allocates
//     ring output without bound (froze WSL). Fixed by the convergence cap in
//     clip.reconcileSharedVertexCrossings, which aborts that frame into
//     SweepResult.Err.
//   - a PANIC (nil deref in doHorizontal) on a coincident-edge confluence in one
//     frame. Fixed by selfUnionAt recovering a failed frame as nil.
//
// In both cases the rotation vote must still produce a result from the frames
// that resolve cleanly. A regression would hang (caught by the test timeout) or
// panic, so reaching a non-empty result is the assertion.
func TestSelfUnionPositiveRobustDegenerate(t *testing.T) {
	cases := []struct {
		name  string
		rings []geom.Polygon
	}{
		{
			// Reconcile non-convergence spin/OOM in the ang=0.21 frame.
			name: "spin",
			rings: []geom.Polygon{
				geom.New().Point(0, 0).Point(3, 0).Point(3, 1).Point(2, 1).Point(2, 6).Point(1, 6).Point(1, 1).Point(0, 1).MustPolygon(),
				geom.New().Point(0, -3).Point(1, -3).Point(1, 2).Point(0, 2).MustPolygon(),
			},
		},
		{
			// doHorizontal nil-deref panic in the ang=0 (axis-aligned) frame.
			name: "panic",
			rings: []geom.Polygon{
				geom.New().Point(0, 0).Point(5, 0).Point(5, 4).Point(4, 4).Point(4, 1).Point(3, 1).Point(3, 6).Point(2, 6).Point(1, 6).Point(1, 2).Point(0, 2).MustPolygon(),
				geom.New().Point(3, 2).Point(9, 2).Point(9, 3).Point(8, 3).Point(8, 4).Point(7, 4).Point(7, 6).Point(6, 6).Point(6, 4).Point(5, 4).Point(5, 5).Point(4, 5).Point(4, 7).Point(3, 7).MustPolygon(),
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := selfUnionPositive(tc.rings)
			require.False(t, len(res) == 0 || res.Area() <= 0, "selfUnionPositive returned no usable region: pieces=%d area=%v", len(res), res.Area())
		})
	}
}
