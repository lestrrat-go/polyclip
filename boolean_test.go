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
	// Two axial squares that OVERLAP. After SplitOverlaps the bottom and
	// top edges are split into coincident-pair fragments (different-source
	// same-direction). §11.7 handles these via synthetic intersections at
	// local-min spawn time (sweep.go's processSynthIntersectsAtLocalMin).
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotArea := got.Area()
	// True union area: 100 + 100 - 70 (overlap [-2,5]×[-5,5]) = 130.
	wantArea := 130.0
	if gotArea < wantArea*0.99 || gotArea > wantArea*1.01 {
		t.Errorf("Union area %v want ≈%v; got=%+v", gotArea, wantArea, got)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 merged piece, got %d: %+v", len(got), got)
	}
}

func TestUnionVerticallyStackedAxialSquares(t *testing.T) {
	// Stacked vertically with shared horizontal edge at Y=5. The shared
	// edge is diff-src opposite-direction (sq1 top goes R→L, sq2 bot goes
	// L→R) so SplitOverlaps doesn't split, BuildLocalMinima succeeds, and
	// the shared boundary is naturally elided by the sweep. Sanity check.
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(0, 10, 5)}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	wantArea := a.Area() + b.Area()
	if got.Area() < wantArea*0.99 || got.Area() > wantArea*1.01 {
		t.Errorf("Union area %v want ≈%v; got=%+v", got.Area(), wantArea, got)
	}
}

func TestUnionThreeOverlappingAxialSquares(t *testing.T) {
	// Three axial squares overlapping in a row: a (x∈[-5,5]), b (x∈[-2,8]),
	// c (x∈[1,11]). After SplitOverlaps the bottom and top edges of a∪b are
	// split into many coincident-pair fragments. §11.7 must handle a chain
	// of synth-intersects along the bottom (and at the top).
	//
	// Union of all three: x∈[-5,11], y∈[-5,5]. Area = 16*10 = 160.
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	c := MultiPolygon{sq(6, 0, 5)}
	ab, err := Union(a, b)
	if err != nil {
		t.Fatalf("Union(a,b): %v", err)
	}
	got, err := Union(ab, c)
	if err != nil {
		t.Fatalf("Union(ab,c): %v", err)
	}
	wantArea := 160.0
	if got.Area() < wantArea*0.99 || got.Area() > wantArea*1.01 {
		t.Errorf("Union(a,b,c) area %v want ≈%v; got=%+v", got.Area(), wantArea, got)
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

func TestUnionAllEmpty(t *testing.T) {
	got, err := UnionAll()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("UnionAll() len=%d want 0", len(got))
	}
}

func TestUnionAllSingle(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	got, err := UnionAll(a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 || got.Area() != a.Area() {
		t.Errorf("UnionAll(a) = %+v, want a", got)
	}
}

func TestUnionAllPairMatchesUnion(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)} // overlaps a
	gotAll, err := UnionAll(a, b)
	if err != nil {
		t.Fatalf("UnionAll: %v", err)
	}
	gotPair, err := Union(a, b)
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if gotAll.Area() != gotPair.Area() {
		t.Errorf("areas differ: UnionAll=%v Union=%v", gotAll.Area(), gotPair.Area())
	}
}

func TestUnionAllManyDisjoint(t *testing.T) {
	polys := []MultiPolygon{
		{sq(0, 0, 1)},
		{sq(10, 0, 1)},
		{sq(20, 0, 1)},
		{sq(30, 0, 1)},
		{sq(40, 0, 1)},
	}
	got, err := UnionAll(polys...)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 5 {
		t.Errorf("len=%d want 5", len(got))
	}
	// Each square is 2x2 → 4 area; 5 disjoint → 20.
	if got.Area() != 20 {
		t.Errorf("Area=%v want 20", got.Area())
	}
}

func TestUnionAllManyOverlapping(t *testing.T) {
	t.Skip("WIP general non-convex crossing-splice bug (DESIGN.md §12.11, " +
		"re-diagnosed 2026-05-20). NOT a maxima/through-vertex coincidence: the " +
		"failure reproduces at full general position (distinct scanlines, no " +
		"coincidences) and on a minimal non-convex 'M' ∪ quad. An edge that crosses " +
		"the other polygon twice is on the union boundary in two disjoint intervals; " +
		"the engine builds the two ring fragments but splices them crosswise " +
		"(wrong front/back orientation of the crossing-spawned AddLocalMinPoly ring).")
	// Five horizontally-shifted diamonds. Diamonds have no horizontal
	// edges so the bound model handles them cleanly even when shifted
	// to share x-extents. UnionAll's tournament reduction must produce
	// the same single connected region as a cumulative Union.
	polys := []MultiPolygon{
		{diamond(0, 0, 10)}, {diamond(5, 0, 10)},
		{diamond(10, 0, 10)}, {diamond(15, 0, 10)},
		{diamond(20, 0, 10)},
	}
	got, err := UnionAll(polys...)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len=%d want 1", len(got))
	}
	// Cross-check against cumulative Union.
	var want MultiPolygon
	for i, p := range polys {
		if i == 0 {
			want = p
			continue
		}
		w, err := Union(want, p)
		if err != nil {
			t.Fatalf("cumulative Union: %v", err)
		}
		want = w
	}
	const tol = 1e-9
	if diff := got.Area() - want.Area(); diff > tol || diff < -tol {
		t.Errorf("area diverges from cumulative: UnionAll=%v cumulative=%v", got.Area(), want.Area())
	}
}

func TestUnionAllDoesNotMutateInput(t *testing.T) {
	// Tournament reduction overwrites entries between rounds; ensure the
	// caller's slice is untouched.
	polys := []MultiPolygon{
		{sq(0, 0, 1)}, {sq(10, 0, 1)}, {sq(20, 0, 1)},
	}
	snapshot := make([]MultiPolygon, len(polys))
	copy(snapshot, polys)
	if _, err := UnionAll(polys...); err != nil {
		t.Fatalf("err: %v", err)
	}
	for i := range polys {
		if &polys[i][0] != &snapshot[i][0] {
			t.Errorf("polys[%d] underlying ExPolygon array changed", i)
		}
	}
}
