package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
	"github.com/stretchr/testify/require"
)

func seg(x1, y1, x2, y2 int64) Segment {
	return NewSegment(
		fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y1)},
		fixed.Point{X: fixed.Coord(x2), Y: fixed.Coord(y2)},
		Subject,
	)
}

func TestIntersect(t *testing.T) {
	pt := func(x, y int64) *fixed.Point {
		return &fixed.Point{X: fixed.Coord(x), Y: fixed.Coord(y)}
	}
	cases := []struct {
		name     string
		a, b     Segment
		wantKind Crossing
		wantP    *fixed.Point // assert r.P only when non-nil
		wantQ    *fixed.Point // assert r.Q only when non-nil
	}{
		{
			name:     "ProperCross",
			a:        seg(0, 0, 10, 10),
			b:        seg(0, 10, 10, 0),
			wantKind: ProperCross,
			wantP:    pt(5, 5),
		},
		{
			name:     "TouchAtEndpoint",
			a:        seg(0, 0, 10, 0),
			b:        seg(5, 0, 5, 10),
			wantKind: Touch,
			wantP:    pt(5, 0),
		},
		{
			name:     "SharedEndpoint",
			a:        seg(0, 0, 5, 5),
			b:        seg(5, 5, 10, 0),
			wantKind: Touch,
			wantP:    pt(5, 5),
		},
		{
			name:     "CollinearOverlap",
			a:        seg(0, 0, 10, 0),
			b:        seg(5, 0, 15, 0),
			wantKind: CollinearOverlap,
			wantP:    pt(5, 0),
			wantQ:    pt(10, 0),
		},
		{
			// Two collinear segments meeting at exactly one point.
			name:     "CollinearTouch",
			a:        seg(0, 0, 5, 0),
			b:        seg(5, 0, 10, 0),
			wantKind: Touch,
			wantP:    pt(5, 0),
		},
		{
			name:     "CollinearDisjoint",
			a:        seg(0, 0, 5, 0),
			b:        seg(10, 0, 15, 0),
			wantKind: NoCrossing,
		},
		{
			name:     "Parallel",
			a:        seg(0, 0, 10, 10),
			b:        seg(0, 5, 10, 15), // parallel, offset
			wantKind: NoCrossing,
		},
		{
			name:     "NonParallelNoOverlap",
			a:        seg(0, 0, 1, 0),
			b:        seg(10, 10, 11, 11), // far away, not parallel
			wantKind: NoCrossing,
		},
		{
			// b's bottom endpoint lies in the interior of a.
			name:     "TJunction",
			a:        seg(0, 0, 10, 0),
			b:        seg(5, 0, 5, 10),
			wantKind: Touch,
			wantP:    pt(5, 0),
		},
		{
			name:     "CollinearVertical",
			a:        seg(3, 0, 3, 10),
			b:        seg(3, 5, 3, 15),
			wantKind: CollinearOverlap,
			wantP:    pt(3, 5),
			wantQ:    pt(3, 10),
		},
		{
			name:     "CollinearContained",
			a:        seg(0, 0, 10, 0),
			b:        seg(3, 0, 7, 0),
			wantKind: CollinearOverlap,
			wantP:    pt(3, 0),
			wantQ:    pt(7, 0),
		},
		{
			// Both segments start at the origin.
			name:     "AtOrigin",
			a:        seg(0, 0, 10, 0),
			b:        seg(0, 0, 0, 10),
			wantKind: Touch,
			wantP:    pt(0, 0),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Intersect(tc.a, tc.b)
			require.Equal(t, tc.wantKind, r.Kind, "Kind: %v want %v", r.Kind, tc.wantKind)
			if tc.wantP != nil {
				require.Equal(t, *tc.wantP, r.P, "P: %+v want %+v", r.P, *tc.wantP)
			}
			if tc.wantQ != nil {
				require.Equal(t, *tc.wantQ, r.Q, "Q: %+v want %+v", r.Q, *tc.wantQ)
			}
		})
	}
}

func TestIntersectProperCrossOrderIndependent(t *testing.T) {
	// The proper-crossing point is parametrised along the first argument, so
	// float rounding of a+t*dir vs b+u*dir can land the same geometric crossing
	// on grid points one unit apart depending on argument order. doIntersections
	// computes the same crossing in adjacent beams with the edges swapped; an
	// order-dependent result escapes the beam's already-handled guard and gets
	// dispatched twice (FuzzIntersect/ff9aee9b909462b0). properIntersection now
	// orders its arguments canonically, so the result must be identical both ways.
	cases := [][2]Segment{
		{seg(0, 0, 10, 10), seg(0, 10, 10, 0)},
		{seg(-10, -7, 48, 51), seg(3, -114, 3, 99)},
		{seg(-100, 3, 100, 7), seg(1, -50, -3, 60)},
		{seg(-7, -3, 11, 41), seg(-30, 5, 22, -9)},
	}
	for i, c := range cases {
		ab := Intersect(c[0], c[1])
		ba := Intersect(c[1], c[0])
		require.True(t, ab.Kind == ProperCross && ba.Kind == ProperCross, "case %d: not a proper cross (%v,%v)", i, ab.Kind, ba.Kind)
		require.Equal(t, ba.P, ab.P, "case %d: order-dependent crossing %+v vs %+v", i, ab.P, ba.P)
	}
}
