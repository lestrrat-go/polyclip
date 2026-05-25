package polyclip

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func square(cx, cy, half float64) Polygon {
	return Polygon{
		{X: cx - half, Y: cy - half},
		{X: cx + half, Y: cy - half},
		{X: cx + half, Y: cy + half},
		{X: cx - half, Y: cy + half},
	}
}

func TestPolygonSignedArea(t *testing.T) {
	ccw := square(0, 0, 5) // CCW in (Y-up) convention
	require.Equal(t, 100.0, ccw.SignedArea(), "ccw SignedArea: got %v want 100", ccw.SignedArea())
	cw := Polygon{{X: -5, Y: -5}, {X: -5, Y: 5}, {X: 5, Y: 5}, {X: 5, Y: -5}} // CW
	require.Equal(t, -100.0, cw.SignedArea(), "cw SignedArea: got %v want -100", cw.SignedArea())
	// Triangle.
	tri := Polygon{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 0, Y: 3}}
	require.Equal(t, 6.0, tri.SignedArea(), "tri SignedArea: got %v want 6", tri.SignedArea())
	// Degenerate.
	require.Equal(t, 0.0, (Polygon{}).SignedArea(), "empty SignedArea: want 0")
	require.Equal(t, 0.0, (Polygon{{X: 1, Y: 2}, {X: 3, Y: 4}}).SignedArea(), "2-point SignedArea: want 0")
}

func TestPolygonArea(t *testing.T) {
	for _, p := range []Polygon{square(0, 0, 5), {{X: -5, Y: -5}, {X: -5, Y: 5}, {X: 5, Y: 5}, {X: 5, Y: -5}}} {
		require.Equal(t, 100.0, p.Area(), "Area: got %v want 100", p.Area())
	}
}

func TestPolygonIsCCW(t *testing.T) {
	require.True(t, square(0, 0, 1).IsCCW(), "square (Y-up) should be CCW")
	cw := Polygon{{X: -1, Y: -1}, {X: -1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: -1}}
	require.False(t, cw.IsCCW(), "cw should not be CCW")
}

func TestPolygonReverse(t *testing.T) {
	p := square(0, 0, 1)
	want := Polygon{p[3], p[2], p[1], p[0]}
	p.Reverse()
	for i := range p {
		require.Equal(t, want[i], p[i], "Reverse[%d]: got %v want %v", i, p[i], want[i])
	}
	// Odd length.
	q := Polygon{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	q.Reverse()
	require.Equal(t, Polygon{{X: 0, Y: 1}, {X: 1, Y: 0}, {X: 0, Y: 0}}, q, "Reverse odd: %v", q)
}

func TestPolygonBoundingBox(t *testing.T) {
	p := Polygon{{X: 1, Y: -2}, {X: 4, Y: 3}, {X: -1, Y: 5}}
	want := BBox{Min: Point{X: -1, Y: -2}, Max: Point{X: 4, Y: 5}}
	require.Equal(t, want, p.BoundingBox(), "BoundingBox: got %+v want %+v", p.BoundingBox(), want)
	require.True(t, (Polygon{}).BoundingBox().Empty(), "empty Polygon BoundingBox should be empty")
}

func TestPolygonContains(t *testing.T) {
	sq := square(0, 0, 5)
	cases := []struct {
		q  Point
		in bool
		// label is for failure messages.
		label string
	}{
		{Point{X: 0, Y: 0}, true, "centre"},
		{Point{X: 4.999, Y: 4.999}, true, "inside near corner"},
		{Point{X: 5, Y: 5}, true, "corner (boundary)"},
		{Point{X: 5, Y: 0}, true, "edge midpoint"},
		{Point{X: -5, Y: 0}, true, "edge midpoint (left)"},
		{Point{X: 5.001, Y: 0}, false, "just outside"},
		{Point{X: 10, Y: 10}, false, "far outside"},
	}
	for _, c := range cases {
		require.Equal(t, c.in, sq.Contains(c.q), "Contains %s %v: got %v want %v", c.label, c.q, sq.Contains(c.q), c.in)
	}
}

func TestExPolygonContainsHole(t *testing.T) {
	outer := square(0, 0, 10) // 20x20
	hole := square(0, 0, 2)   // 4x4 hole
	hole.Reverse()            // hole CW
	ex := ExPolygon{Outer: outer, Holes: []Polygon{hole}}

	cases := []struct {
		q  Point
		in bool
	}{
		{Point{X: 0, Y: 0}, false},     // inside hole
		{Point{X: 1.999, Y: 0}, false}, // inside hole near boundary
		{Point{X: 2, Y: 0}, true},      // on hole boundary
		{Point{X: 5, Y: 0}, true},      // outside hole, inside outer
		{Point{X: 15, Y: 0}, false},    // outside outer
	}
	for _, c := range cases {
		require.Equal(t, c.in, ex.Contains(c.q), "ExPolygon.Contains %v: got %v want %v", c.q, ex.Contains(c.q), c.in)
	}
}

func TestExPolygonArea(t *testing.T) {
	outer := square(0, 0, 10) // 400
	hole := square(0, 0, 3)   // 36
	hole.Reverse()
	ex := ExPolygon{Outer: outer, Holes: []Polygon{hole}}
	require.Equal(t, float64(400-36), ex.Area(), "ExPolygon.Area: got %v want %v", ex.Area(), 400-36)
}

func TestMultiPolygonBoundingBox(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},
		{Outer: square(10, 10, 2)},
	}
	want := BBox{Min: Point{X: -1, Y: -1}, Max: Point{X: 12, Y: 12}}
	require.Equal(t, want, m.BoundingBox(), "MultiPolygon.BoundingBox: got %+v want %+v", m.BoundingBox(), want)
	require.True(t, (MultiPolygon{}).BoundingBox().Empty(), "empty MultiPolygon BoundingBox should be empty")
}

func TestMultiPolygonArea(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},   // 4
		{Outer: square(10, 10, 2)}, // 16
	}
	require.Equal(t, 20.0, m.Area(), "MultiPolygon.Area: got %v want 20", m.Area())
}

func TestMultiPolygonContains(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},
		{Outer: square(10, 10, 2)},
	}
	require.True(t, m.Contains(Point{X: 0, Y: 0}), "Contains centre of first")
	require.True(t, m.Contains(Point{X: 10, Y: 10}), "Contains centre of second")
	require.False(t, m.Contains(Point{X: 5, Y: 5}), "should not contain gap between")
}

func TestCleanRemovesConsecutiveDuplicates(t *testing.T) {
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 0, Y: 0}, // exact duplicate
		{X: 10, Y: 0},
		{X: 10, Y: 0.0001}, // within tol
		{X: 10, Y: 10},
		{X: 0, Y: 10},
	}}}
	got := in.Clean(0.001, 0)
	require.Len(t, got, 1, "len=%d want 1", len(got))
	require.Len(t, got[0].Outer, 4, "vertex count=%d want 4: %+v", len(got[0].Outer), got[0].Outer)
}

func TestCleanRemovesCollinear(t *testing.T) {
	// Square with three extra collinear vertices on the bottom edge.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 5, Y: 0}, {X: 8, Y: 0}, {X: 10, Y: 0},
		{X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got := in.Clean(1e-9, 0)
	require.Len(t, got, 1, "len=%d want 1", len(got))
	require.Len(t, got[0].Outer, 4, "vertex count=%d want 4 (square): %+v", len(got[0].Outer), got[0].Outer)
}

func TestCleanWrapAroundDuplicate(t *testing.T) {
	// Closing duplicate (first vertex repeated at end) — common when callers
	// store rings as closed paths.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0},
	}}}
	got := in.Clean(0, 0)
	require.Len(t, got[0].Outer, 4, "vertex count=%d want 4 (closing duplicate dropped)", len(got[0].Outer))
}

func TestCleanDropsTinyRing(t *testing.T) {
	in := MultiPolygon{
		ExPolygon{Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}},                     // area 100
		ExPolygon{Outer: Polygon{{X: 100, Y: 100}, {X: 100.1, Y: 100}, {X: 100.1, Y: 100.1}, {X: 100, Y: 100.1}}}, // area 0.01
	}
	got := in.Clean(0, 1.0)
	require.Len(t, got, 1, "len=%d want 1 (tiny piece dropped)", len(got))
}

func TestCleanDropsTinyHole(t *testing.T) {
	in := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, // 100
		Holes: []Polygon{
			{{X: 4, Y: 4}, {X: 4, Y: 6}, {X: 6, Y: 6}, {X: 6, Y: 4}},             // CW hole, area 4
			{{X: 2, Y: 2}, {X: 2, Y: 2.01}, {X: 2.01, Y: 2.01}, {X: 2.01, Y: 2}}, // tiny CW hole, ~0.0001
		},
	}}
	got := in.Clean(0, 1.0)
	require.Len(t, got, 1, "piece dropped unexpectedly")
	require.Len(t, got[0].Holes, 1, "holes=%d want 1 (tiny hole dropped)", len(got[0].Holes))
}

func TestCleanCollapseDegenerate(t *testing.T) {
	// All vertices collinear → ring collapses to nothing.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 10, Y: 0}, {X: 5, Y: 0},
	}}}
	got := in.Clean(1e-9, 0)
	require.Empty(t, got, "degenerate ring not dropped: %+v", got)
}

// Sanity: signed-area sign should be consistent with cross-product winding.
func TestSignedAreaSign(t *testing.T) {
	// Generate a few random simple polygons (rotated squares) and check.
	for k := range 8 {
		theta := float64(k) * math.Pi / 4
		c, s := math.Cos(theta), math.Sin(theta)
		base := Polygon{{X: -1, Y: -1}, {X: 1, Y: -1}, {X: 1, Y: 1}, {X: -1, Y: 1}}
		var rot Polygon
		for _, p := range base {
			rot = append(rot, Point{X: c*p.X - s*p.Y, Y: s*p.X + c*p.Y})
		}
		require.True(t, rot.IsCCW(), "rotated CCW square at theta=%v lost CCW: SignedArea=%v", theta, rot.SignedArea())
	}
}
