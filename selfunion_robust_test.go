package polyclip

import (
	"testing"

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
		rings []Polygon
	}{
		{
			// Reconcile non-convergence spin/OOM in the ang=0.21 frame.
			name: "spin",
			rings: []Polygon{
				{{X: 0, Y: 0}, {X: 3, Y: 0}, {X: 3, Y: 1}, {X: 2, Y: 1}, {X: 2, Y: 6}, {X: 1, Y: 6}, {X: 1, Y: 1}, {X: 0, Y: 1}},
				{{X: 0, Y: -3}, {X: 1, Y: -3}, {X: 1, Y: 2}, {X: 0, Y: 2}},
			},
		},
		{
			// doHorizontal nil-deref panic in the ang=0 (axis-aligned) frame.
			name: "panic",
			rings: []Polygon{
				{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 4}, {X: 4, Y: 4}, {X: 4, Y: 1}, {X: 3, Y: 1}, {X: 3, Y: 6}, {X: 2, Y: 6}, {X: 1, Y: 6}, {X: 1, Y: 2}, {X: 0, Y: 2}},
				{{X: 3, Y: 2}, {X: 9, Y: 2}, {X: 9, Y: 3}, {X: 8, Y: 3}, {X: 8, Y: 4}, {X: 7, Y: 4}, {X: 7, Y: 6}, {X: 6, Y: 6}, {X: 6, Y: 4}, {X: 5, Y: 4}, {X: 5, Y: 5}, {X: 4, Y: 5}, {X: 4, Y: 7}, {X: 3, Y: 7}},
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
