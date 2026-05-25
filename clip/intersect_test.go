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

func TestIntersectProperCross(t *testing.T) {
	a := seg(0, 0, 10, 10)
	b := seg(0, 10, 10, 0)
	r := Intersect(a, b)
	require.Equal(t, ProperCross, r.Kind, "Kind: %v want ProperCross", r.Kind)
	want := fixed.Point{X: 5, Y: 5}
	require.Equal(t, want, r.P, "P: %+v want %+v", r.P, want)
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

func TestIntersectTouchAtEndpoint(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 5, 10)
	r := Intersect(a, b)
	require.Equal(t, Touch, r.Kind, "Kind: %v want Touch", r.Kind)
	want := fixed.Point{X: 5, Y: 0}
	require.Equal(t, want, r.P, "P: %+v want %+v", r.P, want)
}

func TestIntersectSharedEndpoint(t *testing.T) {
	a := seg(0, 0, 5, 5)
	b := seg(5, 5, 10, 0)
	r := Intersect(a, b)
	require.Equal(t, Touch, r.Kind, "Kind: %v want Touch", r.Kind)
	want := fixed.Point{X: 5, Y: 5}
	require.Equal(t, want, r.P, "P: %+v want %+v", r.P, want)
}

func TestIntersectCollinearOverlap(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 15, 0)
	r := Intersect(a, b)
	require.Equal(t, CollinearOverlap, r.Kind, "Kind: %v want CollinearOverlap", r.Kind)
	wantP := fixed.Point{X: 5, Y: 0}
	wantQ := fixed.Point{X: 10, Y: 0}
	require.Equal(t, wantP, r.P, "overlap: P=%v Q=%v want %v %v", r.P, r.Q, wantP, wantQ)
	require.Equal(t, wantQ, r.Q, "overlap: P=%v Q=%v want %v %v", r.P, r.Q, wantP, wantQ)
}

func TestIntersectCollinearTouch(t *testing.T) {
	// Two collinear segments meeting at exactly one point.
	a := seg(0, 0, 5, 0)
	b := seg(5, 0, 10, 0)
	r := Intersect(a, b)
	require.Equal(t, Touch, r.Kind, "Kind: %v want Touch", r.Kind)
	require.Equal(t, fixed.Point{X: 5, Y: 0}, r.P, "P: %v", r.P)
}

func TestIntersectCollinearDisjoint(t *testing.T) {
	a := seg(0, 0, 5, 0)
	b := seg(10, 0, 15, 0)
	r := Intersect(a, b)
	require.Equal(t, NoCrossing, r.Kind, "Kind: %v want NoCrossing", r.Kind)
}

func TestIntersectParallel(t *testing.T) {
	a := seg(0, 0, 10, 10)
	b := seg(0, 5, 10, 15) // parallel, offset
	r := Intersect(a, b)
	require.Equal(t, NoCrossing, r.Kind, "Kind: %v want NoCrossing", r.Kind)
}

func TestIntersectNonParallelNoOverlap(t *testing.T) {
	a := seg(0, 0, 1, 0)
	b := seg(10, 10, 11, 11) // far away, not parallel
	r := Intersect(a, b)
	require.Equal(t, NoCrossing, r.Kind, "Kind: %v want NoCrossing", r.Kind)
}

func TestIntersectTJunction(t *testing.T) {
	// b's bottom endpoint lies in the interior of a.
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 5, 10)
	r := Intersect(a, b)
	require.Equal(t, Touch, r.Kind, "Kind: %v want Touch (T-junction)", r.Kind)
	require.Equal(t, fixed.Point{X: 5, Y: 0}, r.P, "P: %v", r.P)
}

func TestIntersectCollinearVertical(t *testing.T) {
	a := seg(3, 0, 3, 10)
	b := seg(3, 5, 3, 15)
	r := Intersect(a, b)
	require.Equal(t, CollinearOverlap, r.Kind, "Kind: %v want CollinearOverlap", r.Kind)
	require.Equal(t, fixed.Point{X: 3, Y: 5}, r.P, "overlap: %v %v", r.P, r.Q)
	require.Equal(t, fixed.Point{X: 3, Y: 10}, r.Q, "overlap: %v %v", r.P, r.Q)
}

func TestIntersectCollinearContained(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(3, 0, 7, 0)
	r := Intersect(a, b)
	require.Equal(t, CollinearOverlap, r.Kind, "Kind: %v want CollinearOverlap", r.Kind)
	require.Equal(t, fixed.Point{X: 3, Y: 0}, r.P, "overlap: %v %v", r.P, r.Q)
	require.Equal(t, fixed.Point{X: 7, Y: 0}, r.Q, "overlap: %v %v", r.P, r.Q)
}

func TestIntersectAtOrigin(t *testing.T) {
	// Both segments start at the origin.
	a := seg(0, 0, 10, 0)
	b := seg(0, 0, 0, 10)
	r := Intersect(a, b)
	require.Equal(t, Touch, r.Kind, "Kind: %v want Touch", r.Kind)
	require.Equal(t, fixed.Point{X: 0, Y: 0}, r.P, "P: %v", r.P)
}
