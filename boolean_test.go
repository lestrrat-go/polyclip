package polyclip

import (
	"strings"
	"testing"
)

func sq(cx, cy, half float64) ExPolygon {
	return ExPolygon{Outer: Polygon{
		{cx - half, cy - half},
		{cx + half, cy - half},
		{cx + half, cy + half},
		{cx - half, cy + half},
	}}
}

func TestUnionEmptyBoth(t *testing.T) {
	got, err := Union(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len=%d want 0", len(got))
	}
}

func TestUnionEmptyA(t *testing.T) {
	b := MultiPolygon{sq(0, 0, 5)}
	got, err := Union(nil, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Outer[0] != b[0].Outer[0] {
		t.Errorf("Union(empty, b) did not return b: %+v", got)
	}
}

func TestUnionEmptyB(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	got, err := Union(a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Union(a, empty) len=%d want 1", len(got))
	}
}

func TestUnionDisjoint(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}  // X in [-5, 5]
	b := MultiPolygon{sq(20, 0, 5)} // X in [15, 25] — strictly disjoint
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2; got %+v", len(got), got)
	}
	// Area sums should equal the sum of inputs (no overlap).
	wantArea := a.Area() + b.Area()
	if got.Area() != wantArea {
		t.Errorf("Area: %v want %v", got.Area(), wantArea)
	}
}

func TestUnionDisjointWithHole(t *testing.T) {
	// A has a hole; B is far away. Hole structure must be preserved.
	holed := ExPolygon{
		Outer: Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}},
		Holes: []Polygon{{{8, 4}, {6, 4}, {6, 6}, {8, 6}}}, // CW hole
	}
	a := MultiPolygon{holed}
	b := MultiPolygon{sq(100, 100, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2", len(got))
	}
	if len(got[0].Holes) != 1 {
		t.Errorf("hole on first piece lost; got holes=%d", len(got[0].Holes))
	}
}

func TestUnionTouchingBoundary(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}  // right edge at X=5
	b := MultiPolygon{sq(10, 0, 5)} // left edge at X=5 — touches a's right edge
	_, err := Union(a, b)
	if err == nil {
		t.Fatal("Union of boundary-touching inputs should return an error for now")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error wording: %v want mention of 'not yet implemented'", err)
	}
}

func TestUnionOverlapping(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)} // overlaps with a
	_, err := Union(a, b)
	if err == nil {
		t.Fatal("Union of overlapping inputs should return an error for now")
	}
}

func TestUnionPreservesOrder(t *testing.T) {
	// Union(a, b) should output a's pieces first, then b's.
	a := MultiPolygon{sq(0, 0, 1), sq(0, 100, 1)}
	b := MultiPolygon{sq(50, 50, 1)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3", len(got))
	}
	// First two ExPolygons should be a's; third should be b's.
	if got[0].Outer[0].Y != a[0].Outer[0].Y || got[1].Outer[0].Y != a[1].Outer[0].Y {
		t.Errorf("order: a's pieces not first; got=%+v", got)
	}
	if got[2].Outer[0].X != b[0].Outer[0].X {
		t.Errorf("order: b's piece not last; got=%+v", got)
	}
}
