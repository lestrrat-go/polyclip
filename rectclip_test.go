package polyclip

import (
	"math"
	"math/rand"
	"testing"
)

func rectAsPolygon(r BBox) MultiPolygon {
	return MultiPolygon{{Outer: Polygon{
		{X: r.Min.X, Y: r.Min.Y},
		{X: r.Max.X, Y: r.Min.Y},
		{X: r.Max.X, Y: r.Max.Y},
		{X: r.Min.X, Y: r.Max.Y},
	}}}
}

func mpArea(m MultiPolygon) float64 {
	var a float64
	for i := range m {
		a += m[i].Area()
	}
	return a
}

func TestRectClipFullyInside(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	sub := MultiPolygon{{Outer: Polygon{{X: 2, Y: 2}, {X: 6, Y: 2}, {X: 6, Y: 6}, {X: 2, Y: 6}}}}
	got := RectClip(sub, rect)
	if len(got) != 1 {
		t.Fatalf("got %d pieces, want 1", len(got))
	}
	if a := mpArea(got); math.Abs(a-16) > 1e-9 {
		t.Errorf("area = %v, want 16", a)
	}
	if !got[0].Outer.IsCCW() {
		t.Error("outer ring not normalized to CCW")
	}
}

func TestRectClipFullyOutside(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}}
	sub := MultiPolygon{{Outer: Polygon{{X: 20, Y: 20}, {X: 30, Y: 20}, {X: 30, Y: 30}, {X: 20, Y: 30}}}}
	if got := RectClip(sub, rect); len(got) != 0 {
		t.Errorf("got %d pieces, want 0", len(got))
	}
}

func TestRectClipRectInsideSolid(t *testing.T) {
	// A small rect entirely within a large solid polygon clips to the full rect.
	rect := BBox{Min: Point{X: 40, Y: 40}, Max: Point{X: 60, Y: 60}}
	sub := MultiPolygon{{Outer: Polygon{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}}}
	if a := mpArea(RectClip(sub, rect)); math.Abs(a-400) > 1e-9 {
		t.Errorf("area = %v, want 400", a)
	}
}

func TestRectClipCornerOverlap(t *testing.T) {
	// Subject overlaps only the top-right corner region [6,10]x[6,10] of the rect.
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	sub := MultiPolygon{{Outer: Polygon{{X: 6, Y: 6}, {X: 16, Y: 6}, {X: 16, Y: 16}, {X: 6, Y: 16}}}}
	if a := mpArea(RectClip(sub, rect)); math.Abs(a-16) > 1e-9 {
		t.Errorf("area = %v, want 16", a)
	}
}

func TestRectClipHolePreserved(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	sub := MultiPolygon{{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes: []Polygon{{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}}},
	}}
	got := RectClip(sub, rect)
	if len(got) != 1 || len(got[0].Holes) != 1 {
		t.Fatalf("got %d pieces with holes %v, want 1 piece 1 hole", len(got), got)
	}
	if a := mpArea(got); math.Abs(a-(100-16)) > 1e-9 {
		t.Errorf("area = %v, want 84", a)
	}
	if got[0].Holes[0].IsCCW() {
		t.Error("hole not normalized to CW")
	}
}

func TestRectClipConcaveSplitAreaParity(t *testing.T) {
	// A U-shape the rectangle splits into two prongs: Sutherland–Hodgman keeps
	// one seam-joined ring, but the enclosed area must still equal Intersect's.
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 4}}
	u := MultiPolygon{{Outer: Polygon{
		{X: 1, Y: -2}, {X: 3, Y: -2}, {X: 3, Y: 8}, {X: 7, Y: 8},
		{X: 7, Y: -2}, {X: 9, Y: -2}, {X: 9, Y: 12}, {X: 1, Y: 12},
	}}}
	got := RectClip(u, rect)
	want, err := Intersect(u, rectAsPolygon(rect))
	if err != nil {
		t.Fatalf("Intersect: %v", err)
	}
	if g, w := mpArea(got), mpArea(want); math.Abs(g-w) > 1e-9 {
		t.Errorf("area = %v, want %v (Intersect)", g, w)
	}
}

func TestRectClipEmptyRect(t *testing.T) {
	sub := MultiPolygon{{Outer: Polygon{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 5}, {X: 0, Y: 5}}}}
	if got := RectClip(sub, EmptyBBox()); len(got) != 0 {
		t.Errorf("got %d pieces, want 0", len(got))
	}
}

func TestRectClipIntersectParityRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	randInt := func(lo, hi int) float64 { return float64(lo + rng.Intn(hi-lo+1)) }
	checked := 0
	for iter := range 2000 {
		// Random integer triangle (convex → SH output is a single piece, so the
		// area parity with Intersect is exact on the integer grid).
		tri := Polygon{
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
		}
		if tri.Area() < 1 {
			continue
		}
		x0, x1 := randInt(-5, 10), randInt(-5, 10)
		y0, y1 := randInt(-5, 10), randInt(-5, 10)
		if x0 == x1 || y0 == y1 {
			continue
		}
		rect := BBox{
			Min: Point{X: math.Min(x0, x1), Y: math.Min(y0, y1)},
			Max: Point{X: math.Max(x0, x1), Y: math.Max(y0, y1)},
		}
		sub := MultiPolygon{{Outer: tri}}
		got := RectClip(sub, rect)
		want, err := Intersect(sub, rectAsPolygon(rect))
		if err != nil {
			t.Fatalf("iter %d Intersect: %v", iter, err)
		}
		if g, w := mpArea(got), mpArea(want); math.Abs(g-w) > 1e-6 {
			t.Fatalf("iter %d area = %v, want %v; tri=%v rect=%v", iter, g, w, tri, rect)
		}
		checked++
	}
	if checked < 1000 {
		t.Fatalf("only %d non-degenerate cases exercised, want > 1000", checked)
	}
}

func TestRectClipLinesCrossing(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	got := RectClipLines([]Polyline{{{X: -5, Y: 5}, {X: 15, Y: 5}}}, rect)
	want := []Polyline{{{X: 0, Y: 5}, {X: 10, Y: 5}}}
	if !polylinesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRectClipLinesFullyInside(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	in := Polyline{{X: 2, Y: 2}, {X: 5, Y: 8}, {X: 8, Y: 3}}
	got := RectClipLines([]Polyline{in}, rect)
	if !polylinesEqual(got, []Polyline{in}) {
		t.Errorf("got %v, want %v", got, in)
	}
}

func TestRectClipLinesFullyOutside(t *testing.T) {
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	if got := RectClipLines([]Polyline{{{X: 20, Y: 20}, {X: 30, Y: 30}}}, rect); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func TestRectClipLinesReentry(t *testing.T) {
	// A path that dips out of the rect and back in is split into two polylines.
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	path := Polyline{{X: 2, Y: 5}, {X: 2, Y: -5}, {X: 8, Y: -5}, {X: 8, Y: 5}}
	got := RectClipLines([]Polyline{path}, rect)
	want := []Polyline{{{X: 2, Y: 5}, {X: 2, Y: 0}}, {{X: 8, Y: 0}, {X: 8, Y: 5}}}
	if !polylinesEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRectClipLinesTouchVertexStaysJoined(t *testing.T) {
	// A vertex sitting exactly on the boundary must not split the polyline.
	rect := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	path := Polyline{{X: 2, Y: 2}, {X: 5, Y: 0}, {X: 8, Y: 2}}
	got := RectClipLines([]Polyline{path}, rect)
	if !polylinesEqual(got, []Polyline{path}) {
		t.Errorf("got %v, want %v", got, path)
	}
}

func TestRectClipLinesEmptyRect(t *testing.T) {
	if got := RectClipLines([]Polyline{{{X: 0, Y: 0}, {X: 5, Y: 5}}}, EmptyBBox()); len(got) != 0 {
		t.Errorf("got %v, want none", got)
	}
}

func polylinesEqual(a, b []Polyline) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
