package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func TestBuildLocalMinima(t *testing.T) {
	cases := []struct {
		name string
		// segs builds the input segment slice for the case.
		segs func() []Segment
		// wantVertices lists the expected local-minima Vertex values in
		// order. Its length is the expected number of minima.
		wantVertices []fixed.Point
		// extra runs case-specific structural assertions (bounds, initial
		// X/slope). Optional; nil means no extra checks.
		extra func(t *testing.T, minima []LocalMin)
	}{
		{
			// CCW axial rectangle: one local min at the bottom-left, one
			// local max at the top-right. Each bound has two segments — one
			// horizontal and one vertical.
			name:         "AxialRectangle",
			segs:         func() []Segment { return axialRect(0, 0, 10, 5, Subject) },
			wantVertices: []fixed.Point{{X: 0, Y: 0}},
			extra: func(t *testing.T, minima []LocalMin) {
				m := minima[0]
				require.NotNil(t, m.Left, "bounds nil: left=%v right=%v", m.Left, m.Right)
				require.NotNil(t, m.Right, "bounds nil: left=%v right=%v", m.Left, m.Right)
				require.Len(t, m.Left.Segs, 2, "left segs: %d want 2; segs=%v", len(m.Left.Segs), m.Left.Segs)
				require.Len(t, m.Right.Segs, 2, "right segs: %d want 2; segs=%v", len(m.Right.Segs), m.Right.Segs)

				// Left bound's first non-horizontal should be at X=0 (left vertical).
				// Right bound's first non-horizontal should be at X=10 (right vertical).
				require.Equal(t, fixed.Coord(0), boundInitialX(m.Left, m.Vertex), "left bound initial X want 0")
				require.Equal(t, fixed.Coord(10), boundInitialX(m.Right, m.Vertex), "right bound initial X want 10")

				// Both bounds must end at the local max (1 of the two top vertices,
				// whichever the down→up transition selected). For axialRect it's the
				// end vertex of the up edge (1,0)→(1,5), which is (10,5).
				leftLast := m.Left.Last()
				rightLast := m.Right.Last()
				topY := fixed.Coord(5)
				require.True(t, leftLast.Top.Y == topY && rightLast.Top.Y == topY,
					"bounds last edge top Y: left=%v right=%v want both at Y=%d",
					leftLast.Top, rightLast.Top, int64(topY))
			},
		},
		{
			// CCW diamond: one local min at the bottom vertex, one local max
			// at the top. Each bound has two non-horizontal segments.
			name:         "Diamond",
			segs:         func() []Segment { return diamond(0, 0, 10, Subject) },
			wantVertices: []fixed.Point{{X: 0, Y: -10}},
			extra: func(t *testing.T, minima []LocalMin) {
				m := minima[0]
				require.Len(t, m.Left.Segs, 2, "left segs: %d want 2", len(m.Left.Segs))
				require.Len(t, m.Right.Segs, 2, "right segs: %d want 2", len(m.Right.Segs))
				// Left bound's first segment goes UP-LEFT from local min: (0,-10) →
				// (-10,0). Slope -1.
				// Right bound's first goes UP-RIGHT: (0,-10) → (10,0). Slope +1.
				require.Negative(t, boundInitialSlope(m.Left), "left bound initial slope want negative")
				require.Positive(t, boundInitialSlope(m.Right), "right bound initial slope want positive")
			},
		},
		{
			// Two CCW axial rectangles, no shared vertices. Two local minima
			// expected, sorted by (Y, X): (0,0) first (Y=0), then (20,10).
			name: "TwoDisjointRectangles",
			segs: func() []Segment {
				var segs []Segment
				segs = append(segs, axialRect(0, 0, 5, 3, Subject)...)
				segs = append(segs, axialRect(20, 10, 25, 13, Clip)...)
				return segs
			},
			wantVertices: []fixed.Point{{X: 0, Y: 0}, {X: 20, Y: 10}},
		},
		{
			// Outer CCW axial rectangle + inner CCW axial rectangle (NOT a
			// hole; just two independent rings as far as BuildLocalMinima is
			// concerned). Two local minima — both at the bottom-left of their
			// respective rectangles.
			name: "NestedRectangles",
			segs: func() []Segment {
				var segs []Segment
				segs = append(segs, axialRect(0, 0, 20, 20, Subject)...)
				segs = append(segs, axialRect(5, 5, 15, 15, Subject)...)
				return segs
			},
			wantVertices: []fixed.Point{{X: 0, Y: 0}, {X: 5, Y: 5}},
		},
		{
			// Two squares sharing a corner at (5,5) — different sources. The
			// source-based disambiguation in pickNextSegment keeps each ring's
			// trace within its own source. Two local minima expected, one per
			// square's bottom-left vertex.
			name: "SharedVertex",
			segs: func() []Segment {
				var segs []Segment
				segs = append(segs, axialRect(0, 0, 5, 5, Subject)...)
				segs = append(segs, axialRect(5, 5, 10, 10, Clip)...)
				return segs
			},
			wantVertices: []fixed.Point{{X: 0, Y: 0}, {X: 5, Y: 5}},
		},
		{
			name:         "Empty",
			segs:         func() []Segment { return nil },
			wantVertices: nil,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			minima, err := BuildLocalMinima(tc.segs())
			require.NoError(t, err)
			require.Len(t, minima, len(tc.wantVertices), "expected %d local minima, got %d", len(tc.wantVertices), len(minima))
			for i, want := range tc.wantVertices {
				require.Equal(t, want, minima[i].Vertex, "minima[%d] vertex: %v want %v", i, minima[i].Vertex, want)
			}
			if tc.extra != nil {
				tc.extra(t, minima)
			}
		})
	}
}

func TestBuildLocalMinimaWShape(t *testing.T) {
	// CCW "W"-like polygon with TWO local minima (bottom-left of the W and
	// bottom-right) and ONE local maximum (top of the W, at the central
	// dip-up). Vertices (sloped, no horizontals):
	//   v0(0,0) — bottom-left local min
	//   v1(5,5) — central peak (local max for the left half, but...)
	//   v2(10,0) — bottom-right local min
	//   v3(10,20) — top-right corner (true local max)
	//   v4(0,20) — top-left corner
	// Walking CCW: v0 → v2 → v3 → v4 → v1 → v0? No, let me reconsider.
	//
	// Simpler W: two valleys joined by a peak.
	//   v0(0,10) — top-left
	//   v1(2,0)  — bottom-left local min
	//   v2(5,8)  — middle peak (local max)
	//   v3(8,0)  — bottom-right local min
	//   v4(10,10) — top-right
	// CCW: v4 → v3 → v2 → v1 → v0 → v4. (Going right-to-left along the top,
	// then down-up-down-up zigzag along the bottom.)
	pts := []fixed.Point{
		{X: 10, Y: 10}, // v4 top-right
		{X: 8, Y: 0},   // v3
		{X: 5, Y: 8},   // v2 peak
		{X: 2, Y: 0},   // v1
		{X: 0, Y: 10},  // v0 top-left
	}
	n := len(pts)
	segs := make([]Segment, 0, n)
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		seg := NewSegment(pts[i], pts[j], Subject)
		if !seg.Degenerate() {
			segs = append(segs, seg)
		}
	}
	minima, err := BuildLocalMinima(segs)
	require.NoError(t, err)
	require.Len(t, minima, 2, "expected 2 local minima, got %d; minima=%v", len(minima), minima)
	// Sorted by (Y, X): both at Y=0, X=2 then X=8.
	require.Equal(t, fixed.Point{X: 2, Y: 0}, minima[0].Vertex, "first min: %v want (2,0)", minima[0].Vertex)
	require.Equal(t, fixed.Point{X: 8, Y: 0}, minima[1].Vertex, "second min: %v want (8,0)", minima[1].Vertex)
}
