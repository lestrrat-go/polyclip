package polyclip

import (
	"math"
	"testing"
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
	if got := ccw.SignedArea(); got != 100 {
		t.Errorf("ccw SignedArea: got %v want 100", got)
	}
	cw := Polygon{{X: -5, Y: -5}, {X: -5, Y: 5}, {X: 5, Y: 5}, {X: 5, Y: -5}} // CW
	if got := cw.SignedArea(); got != -100 {
		t.Errorf("cw SignedArea: got %v want -100", got)
	}
	// Triangle.
	tri := Polygon{{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 0, Y: 3}}
	if got := tri.SignedArea(); got != 6 {
		t.Errorf("tri SignedArea: got %v want 6", got)
	}
	// Degenerate.
	if got := (Polygon{}).SignedArea(); got != 0 {
		t.Errorf("empty SignedArea: %v want 0", got)
	}
	if got := (Polygon{{X: 1, Y: 2}, {X: 3, Y: 4}}).SignedArea(); got != 0 {
		t.Errorf("2-point SignedArea: %v want 0", got)
	}
}

func TestPolygonArea(t *testing.T) {
	for _, p := range []Polygon{square(0, 0, 5), {{X: -5, Y: -5}, {X: -5, Y: 5}, {X: 5, Y: 5}, {X: 5, Y: -5}}} {
		if got := p.Area(); got != 100 {
			t.Errorf("Area: got %v want 100", got)
		}
	}
}

func TestPolygonIsCCW(t *testing.T) {
	if !square(0, 0, 1).IsCCW() {
		t.Error("square (Y-up) should be CCW")
	}
	cw := Polygon{{X: -1, Y: -1}, {X: -1, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: -1}}
	if cw.IsCCW() {
		t.Error("cw should not be CCW")
	}
}

func TestPolygonReverse(t *testing.T) {
	p := square(0, 0, 1)
	want := Polygon{p[3], p[2], p[1], p[0]}
	p.Reverse()
	for i := range p {
		if p[i] != want[i] {
			t.Fatalf("Reverse[%d]: got %v want %v", i, p[i], want[i])
		}
	}
	// Odd length.
	q := Polygon{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0, Y: 1}}
	q.Reverse()
	if q[0] != (Point{X: 0, Y: 1}) || q[1] != (Point{X: 1, Y: 0}) || q[2] != (Point{X: 0, Y: 0}) {
		t.Errorf("Reverse odd: %v", q)
	}
}

func TestPolygonBoundingBox(t *testing.T) {
	p := Polygon{{X: 1, Y: -2}, {X: 4, Y: 3}, {X: -1, Y: 5}}
	want := BBox{Min: Point{X: -1, Y: -2}, Max: Point{X: 4, Y: 5}}
	if got := p.BoundingBox(); got != want {
		t.Errorf("BoundingBox: got %+v want %+v", got, want)
	}
	if !(Polygon{}).BoundingBox().Empty() {
		t.Error("empty Polygon BoundingBox should be empty")
	}
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
		if got := sq.Contains(c.q); got != c.in {
			t.Errorf("Contains %s %v: got %v want %v", c.label, c.q, got, c.in)
		}
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
		if got := ex.Contains(c.q); got != c.in {
			t.Errorf("ExPolygon.Contains %v: got %v want %v", c.q, got, c.in)
		}
	}
}

func TestExPolygonArea(t *testing.T) {
	outer := square(0, 0, 10) // 400
	hole := square(0, 0, 3)   // 36
	hole.Reverse()
	ex := ExPolygon{Outer: outer, Holes: []Polygon{hole}}
	if got := ex.Area(); got != 400-36 {
		t.Errorf("ExPolygon.Area: got %v want %v", got, 400-36)
	}
}

func TestMultiPolygonBoundingBox(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},
		{Outer: square(10, 10, 2)},
	}
	want := BBox{Min: Point{X: -1, Y: -1}, Max: Point{X: 12, Y: 12}}
	if got := m.BoundingBox(); got != want {
		t.Errorf("MultiPolygon.BoundingBox: got %+v want %+v", got, want)
	}
	if !(MultiPolygon{}).BoundingBox().Empty() {
		t.Error("empty MultiPolygon BoundingBox should be empty")
	}
}

func TestMultiPolygonArea(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},   // 4
		{Outer: square(10, 10, 2)}, // 16
	}
	if got := m.Area(); got != 20 {
		t.Errorf("MultiPolygon.Area: got %v want 20", got)
	}
}

func TestMultiPolygonContains(t *testing.T) {
	m := MultiPolygon{
		{Outer: square(0, 0, 1)},
		{Outer: square(10, 10, 2)},
	}
	if !m.Contains(Point{X: 0, Y: 0}) {
		t.Error("Contains centre of first")
	}
	if !m.Contains(Point{X: 10, Y: 10}) {
		t.Error("Contains centre of second")
	}
	if m.Contains(Point{X: 5, Y: 5}) {
		t.Error("should not contain gap between")
	}
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
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if n := len(got[0].Outer); n != 4 {
		t.Errorf("vertex count=%d want 4: %+v", n, got[0].Outer)
	}
}

func TestCleanRemovesCollinear(t *testing.T) {
	// Square with three extra collinear vertices on the bottom edge.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 5, Y: 0}, {X: 8, Y: 0}, {X: 10, Y: 0},
		{X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got := in.Clean(1e-9, 0)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	if n := len(got[0].Outer); n != 4 {
		t.Errorf("vertex count=%d want 4 (square): %+v", n, got[0].Outer)
	}
}

func TestCleanWrapAroundDuplicate(t *testing.T) {
	// Closing duplicate (first vertex repeated at end) — common when callers
	// store rings as closed paths.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0},
	}}}
	got := in.Clean(0, 0)
	if n := len(got[0].Outer); n != 4 {
		t.Errorf("vertex count=%d want 4 (closing duplicate dropped)", n)
	}
}

func TestCleanDropsTinyRing(t *testing.T) {
	in := MultiPolygon{
		ExPolygon{Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}},                     // area 100
		ExPolygon{Outer: Polygon{{X: 100, Y: 100}, {X: 100.1, Y: 100}, {X: 100.1, Y: 100.1}, {X: 100, Y: 100.1}}}, // area 0.01
	}
	got := in.Clean(0, 1.0)
	if len(got) != 1 {
		t.Fatalf("len=%d want 1 (tiny piece dropped)", len(got))
	}
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
	if len(got) != 1 {
		t.Fatalf("piece dropped unexpectedly")
	}
	if len(got[0].Holes) != 1 {
		t.Errorf("holes=%d want 1 (tiny hole dropped)", len(got[0].Holes))
	}
}

func TestCleanCollapseDegenerate(t *testing.T) {
	// All vertices collinear → ring collapses to nothing.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 10, Y: 0}, {X: 5, Y: 0},
	}}}
	got := in.Clean(1e-9, 0)
	if len(got) != 0 {
		t.Errorf("degenerate ring not dropped: %+v", got)
	}
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
		if !rot.IsCCW() {
			t.Errorf("rotated CCW square at theta=%v lost CCW: SignedArea=%v", theta, rot.SignedArea())
		}
	}
}
