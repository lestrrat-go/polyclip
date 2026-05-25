package polyclip

import "testing"

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
				{{0, 0}, {3, 0}, {3, 1}, {2, 1}, {2, 6}, {1, 6}, {1, 1}, {0, 1}},
				{{0, -3}, {1, -3}, {1, 2}, {0, 2}},
			},
		},
		{
			// doHorizontal nil-deref panic in the ang=0 (axis-aligned) frame.
			name: "panic",
			rings: []Polygon{
				{{0, 0}, {5, 0}, {5, 4}, {4, 4}, {4, 1}, {3, 1}, {3, 6}, {2, 6}, {1, 6}, {1, 2}, {0, 2}},
				{{3, 2}, {9, 2}, {9, 3}, {8, 3}, {8, 4}, {7, 4}, {7, 6}, {6, 6}, {6, 4}, {5, 4}, {5, 5}, {4, 5}, {4, 7}, {3, 7}},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := selfUnionPositive(tc.rings)
			if len(res) == 0 || res.Area() <= 0 {
				t.Fatalf("selfUnionPositive returned no usable region: pieces=%d area=%v", len(res), res.Area())
			}
		})
	}
}
