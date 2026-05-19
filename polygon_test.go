package polyclip

import (
	"math"
	"testing"
)

func square(cx, cy, half float64) Polygon {
	return Polygon{
		{cx - half, cy - half},
		{cx + half, cy - half},
		{cx + half, cy + half},
		{cx - half, cy + half},
	}
}

func TestPolygonSignedArea(t *testing.T) {
	ccw := square(0, 0, 5) // CCW in (Y-up) convention
	if got := ccw.SignedArea(); got != 100 {
		t.Errorf("ccw SignedArea: got %v want 100", got)
	}
	cw := Polygon{{-5, -5}, {-5, 5}, {5, 5}, {5, -5}} // CW
	if got := cw.SignedArea(); got != -100 {
		t.Errorf("cw SignedArea: got %v want -100", got)
	}
	// Triangle.
	tri := Polygon{{0, 0}, {4, 0}, {0, 3}}
	if got := tri.SignedArea(); got != 6 {
		t.Errorf("tri SignedArea: got %v want 6", got)
	}
	// Degenerate.
	if got := (Polygon{}).SignedArea(); got != 0 {
		t.Errorf("empty SignedArea: %v want 0", got)
	}
	if got := (Polygon{{1, 2}, {3, 4}}).SignedArea(); got != 0 {
		t.Errorf("2-point SignedArea: %v want 0", got)
	}
}

func TestPolygonArea(t *testing.T) {
	for _, p := range []Polygon{square(0, 0, 5), {{-5, -5}, {-5, 5}, {5, 5}, {5, -5}}} {
		if got := p.Area(); got != 100 {
			t.Errorf("Area: got %v want 100", got)
		}
	}
}

func TestPolygonIsCCW(t *testing.T) {
	if !square(0, 0, 1).IsCCW() {
		t.Error("square (Y-up) should be CCW")
	}
	cw := Polygon{{-1, -1}, {-1, 1}, {1, 1}, {1, -1}}
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
	q := Polygon{{0, 0}, {1, 0}, {0, 1}}
	q.Reverse()
	if q[0] != (Point{0, 1}) || q[1] != (Point{1, 0}) || q[2] != (Point{0, 0}) {
		t.Errorf("Reverse odd: %v", q)
	}
}

func TestPolygonBoundingBox(t *testing.T) {
	p := Polygon{{1, -2}, {4, 3}, {-1, 5}}
	want := BBox{Min: Point{-1, -2}, Max: Point{4, 5}}
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
		{Point{0, 0}, true, "centre"},
		{Point{4.999, 4.999}, true, "inside near corner"},
		{Point{5, 5}, true, "corner (boundary)"},
		{Point{5, 0}, true, "edge midpoint"},
		{Point{-5, 0}, true, "edge midpoint (left)"},
		{Point{5.001, 0}, false, "just outside"},
		{Point{10, 10}, false, "far outside"},
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
		{Point{0, 0}, false},     // inside hole
		{Point{1.999, 0}, false}, // inside hole near boundary
		{Point{2, 0}, true},      // on hole boundary
		{Point{5, 0}, true},      // outside hole, inside outer
		{Point{15, 0}, false},    // outside outer
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
	want := BBox{Min: Point{-1, -1}, Max: Point{12, 12}}
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
	if !m.Contains(Point{0, 0}) {
		t.Error("Contains centre of first")
	}
	if !m.Contains(Point{10, 10}) {
		t.Error("Contains centre of second")
	}
	if m.Contains(Point{5, 5}) {
		t.Error("should not contain gap between")
	}
}

// Sanity: signed-area sign should be consistent with cross-product winding.
func TestSignedAreaSign(t *testing.T) {
	// Generate a few random simple polygons (rotated squares) and check.
	for k := 0; k < 8; k++ {
		theta := float64(k) * math.Pi / 4
		c, s := math.Cos(theta), math.Sin(theta)
		base := Polygon{{-1, -1}, {1, -1}, {1, 1}, {-1, 1}}
		var rot Polygon
		for _, p := range base {
			rot = append(rot, Point{X: c*p.X - s*p.Y, Y: s*p.X + c*p.Y})
		}
		if !rot.IsCCW() {
			t.Errorf("rotated CCW square at theta=%v lost CCW: SignedArea=%v", theta, rot.SignedArea())
		}
	}
}
