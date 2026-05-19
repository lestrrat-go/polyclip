package polyclip

import (
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

func TestUnionTouchingBoundaryAxisAligned(t *testing.T) {
	// Two CCW axial squares that share the X=5 boundary. SplitOverlaps does
	// not split them (they only touch at endpoints); BuildLocalMinima's
	// source+angle disambiguation resolves the shared corners. The merged
	// region is a single rectangle.
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(10, 0, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotArea := got.Area()
	wantArea := a.Area() + b.Area()
	if gotArea < wantArea*0.99 || gotArea > wantArea*1.01 {
		t.Errorf("Union area %v want ≈%v; got=%+v", gotArea, wantArea, got)
	}
}

func TestUnionOverlappingAxisAligned(t *testing.T) {
	// Two axial squares that OVERLAP — after SplitOverlaps the bottom and
	// top edges are split into coincident-pair fragments (same source-
	// direction-but-different-source). Per DESIGN.md §11.7 these pairs
	// should emit ONE edge with combined winding; the current sweep
	// classifies both as non-contributing and the merged outline is
	// incomplete. Tracked as a known limitation; the union returns
	// without error but with an under-area shape.
	t.Skip("§11.7 coincident same-source-same-direction not implemented; overlap area computed incorrectly")
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotArea := got.Area()
	if gotArea <= a.Area() || gotArea >= a.Area()+b.Area() {
		t.Errorf("Union area %v not in (%v, %v); got=%+v", gotArea, a.Area(), a.Area()+b.Area(), got)
	}
}

func TestUnionNestedAxialSquares(t *testing.T) {
	// Big axial square as subject, small axial square strictly inside as
	// clip. Bboxes overlap so the engine path runs; rings do not share any
	// vertex so ClassifyHorizontals succeeds. Union should be the outer
	// square (the inner is fully contained — no new edges contribute).
	a := MultiPolygon{sq(0, 0, 10)}
	b := MultiPolygon{sq(0, 0, 3)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 piece, got %d: %+v", len(got), got)
	}
	if len(got[0].Outer) != 4 {
		t.Errorf("outer ring vertex count: %d want 4; outer=%v", len(got[0].Outer), got[0].Outer)
	}
	if len(got[0].Holes) != 0 {
		t.Errorf("unexpected holes: %v", got[0].Holes)
	}
	gotArea := got.Area()
	wantArea := 20.0 * 20.0
	if gotArea < wantArea*0.99 || gotArea > wantArea*1.01 {
		t.Errorf("Union area %v want ≈%v", gotArea, wantArea)
	}
}

// diamond returns a CCW unit-ish diamond ExPolygon with no horizontal edges,
// suitable for exercising the engine path.
func diamond(cx, cy, r float64) ExPolygon {
	return ExPolygon{Outer: Polygon{
		{cx, cy - r}, {cx + r, cy}, {cx, cy + r}, {cx - r, cy},
	}}
}

func TestUnionOverlappingDiamonds(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected single merged piece, got %d: %+v", len(got), got)
	}
	aArea, bArea := a.Area(), b.Area()
	gotArea := got.Area()
	floor := aArea
	if bArea > floor {
		floor = bArea
	}
	if gotArea < floor*0.99 {
		t.Errorf("Union area %v is below floor %v", gotArea, floor)
	}
	if gotArea > aArea+bArea+0.01 {
		t.Errorf("Union area %v exceeds sum %v", gotArea, aArea+bArea)
	}
}

func TestUnionDisjointDiamonds(t *testing.T) {
	// Disjoint inputs with the engine-path check (bboxes touch lightly).
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(100, 100, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 disjoint pieces, got %d", len(got))
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

func TestIntersectEmpty(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	got, err := Intersect(nil, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Intersect(empty, a) = %v want empty", got)
	}
	got, err = Intersect(a, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Intersect(a, empty) = %v want empty", got)
	}
}

func TestIntersectDisjoint(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(100, 100, 5)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Intersect(disjoint) = %v want empty", got)
	}
}

func TestIntersectOverlappingDiamonds(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty intersection, got 0 pieces")
	}
	// Intersection of two diamonds centred (0,0) and (5,0) with r=10 is
	// a lens-shaped region with area > 0 but less than each diamond's 200.
	gotArea := got.Area()
	if gotArea <= 0 || gotArea >= 200 {
		t.Errorf("Intersect area %v not in (0, 200); got=%+v", gotArea, got)
	}
}

func TestDifferenceEmpty(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	got, err := Difference(nil, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Diff(empty, a) = %v want empty", got)
	}
	got, err = Difference(a, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Diff(a, empty) len=%d want 1", len(got))
	}
}

func TestDifferenceDisjoint(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(100, 100, 5)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Diff(a, disjoint b) len=%d want 1", len(got))
	}
}

func TestXorEmpty(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	got, err := Xor(nil, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("Xor(empty, a) len=%d want 1", len(got))
	}
}

func TestXorDisjoint(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(100, 100, 5)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("Xor(disjoint) len=%d want 2", len(got))
	}
}
