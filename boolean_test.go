package polyclip

import (
	"math"
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

func TestUnionSharedVertexCrossing(t *testing.T) {
	// B's vertex (6,9) lies exactly on A's edge (1,8)->(16,11). SplitTJunctions
	// turns that into a shared vertex where A and B swap left-right order — a
	// real crossing the per-scanbeam intersection pass cannot see (it is a Touch
	// on the beam boundary, not a ProperCross strictly inside the open beam).
	// reconcileSharedVertexCrossings dispatches it. Before the fix the engine
	// reported 75.5; the true union area is ~298.5 (DESIGN.md §12.11).
	a := Polygon{{1, 8}, {16, 11}, {21, 5}, {29, 24}}
	b := Polygon{{6, 9}, {24, 7}, {19, 28}, {13, 29}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Union(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	gotArea := got.Area()
	if gotArea < 290 || gotArea > 305 {
		t.Errorf("Union area %v outside expected band [290,305] (truth ~298.5)", gotArea)
	}
}

func TestUnionSharedVertexViaHorizontal(t *testing.T) {
	// B's bottom horizontal (6,3)-(8,3) ends exactly at A's local-minimum
	// vertex (8,3). doHorizontal does not cross edges at its far endpoint, and
	// the far endpoint is only settled into the AEL after the horizontal flush,
	// so the crossing between B's promoted bound and A's two local-min bounds
	// was never dispatched — the ring threaded through (8,3) twice (a
	// self-touch) and the union over-counted to 9.0. The post-flush
	// reconcileSharedVertexCrossings pass dispatches it; the true union area is
	// ~7.52 (DESIGN.md §12.11, track C).
	a := Polygon{{8, 3}, {9, 5}, {1, 4}, {4, 4}}
	b := Polygon{{6, 3}, {8, 3}, {10, 5}, {5, 4}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Union(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArea := got.Area(); gotArea < 7.2 || gotArea > 7.8 {
		t.Errorf("Union area %v outside expected band [7.2,7.8] (truth ~7.52; pre-fix bug was 9.0)", gotArea)
	}
}

func TestUnionOverlappingSharedVertexMismerge(t *testing.T) {
	// A and B overlap and share exactly one vertex (10,4), which is A's local-max
	// plateau right-end AND a through-vertex of B's right bound. A's right edge
	// (8,0)->(10,4) reaches the shared vertex hot; B's right bound passes through
	// it COLD (interior, having entered A at a lower crossing) and exits there.
	// Before the fix A's maximum closed without handing its ring onto B's
	// continuing edge (10,4)->(11,6), so B's entire upper triangle was dropped and
	// the union collapsed to one malformed ring of area 14.4. handoffMaxThroughVertex
	// dispatches the at-vertex crossing so hotness transfers to B's bound; the true
	// union area is ~43.9 (DESIGN.md §12.11, overlapping shared-vertex mis-merge).
	a := Polygon{{0, 4}, {7, 2}, {8, 0}, {10, 4}}
	b := Polygon{{10, 4}, {11, 6}, {2, 12}, {7, 1}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Union(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArea := got.Area(); gotArea < 43.5 || gotArea > 44.3 {
		t.Errorf("Union area %v outside expected band [43.5,44.3] (truth ~43.9; pre-fix bug was 14.4)", gotArea)
	}
}

func TestUnionThroughVertexBoundLastSegment(t *testing.T) {
	// A and B overlap and share exactly one vertex (9,8): A's local-max plateau
	// right-end (A's right bound (3,6)->(9,8) plus its top horizontal both end
	// there) AND a through-vertex of B's right bound (3,7)->(9,8)->(5,11). B
	// passes through (9,8) COLD, so A's terminating maximum must hand its ring
	// onto B's continuing edge (9,8)->(5,11).
	//
	// This differs from TestUnionOverlappingSharedVertexMismerge in TIMING: by
	// the time A's maximum closes, B's cursor has already advanced onto its FINAL
	// segment (9,8)->(5,11). The old handoff test rejected any bound-last edge,
	// so it skipped this through-edge and dropped B's upper triangle, collapsing
	// the union to 14.75. The qualification now reads the bound's ultimate apex
	// ((5,11), above (9,8)), which is independent of cursor position; truth ~23.23
	// (DESIGN.md §12.11, through-vertex on a bound's last segment).
	a := Polygon{{0, 8}, {5, 3}, {3, 6}, {9, 8}}
	b := Polygon{{2, 9}, {3, 7}, {9, 8}, {5, 11}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Union(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArea := got.Area(); gotArea < 23.0 || gotArea > 23.5 {
		t.Errorf("Union area %v outside expected band [23.0,23.5] (truth ~23.23; pre-fix bug was 14.75)", gotArea)
	}
}

func TestXorHotThroughSharedApexConfluence(t *testing.T) {
	// Same inputs as TestUnionThroughVertexBoundLastSegment, but Xor — the
	// hot-through shared-apex confluence. At the shared vertex (9,8) A reaches
	// its local-max plateau (right bound (3,6)->(9,8) + top horizontal) while B's
	// right bound passes through (9,8) HOT (it carries its own ring across the
	// vertex). The correct Xor is two disjoint CCW rings — (A\B) area 8.25 and
	// (B\A) area 11.75 — touching at (9,8) and the crossing (2.5,8) but NOT
	// merged; total ~20.0.
	//
	// Pre-fix the sweep closed A's right bound (Case A deferred handoff) BEFORE
	// the top horizontal traversed; the horizontal then crossed B's left bound
	// and was SwapOutrecs'd into B's ring, leaving B's two bounds same-side at
	// B's apex (5,11). AddLocalMaxPoly's same-side workaround reversed and joined
	// the two rings into one self-touching ring that visited (9,8) three times,
	// collapsing Xor to 0.25. The fix defers A's plateau maximum until the
	// horizontal reaches it (Clipper2 treats A's right-bound vertex as
	// intermediate), so the horizontal's closeBound pairs cleanly and
	// resolveBetweenMaxima crosses B's hot through-bound at the apex (DESIGN.md
	// §12.11, hot-through shared-apex confluence).
	a := Polygon{{0, 8}, {5, 3}, {3, 6}, {9, 8}}
	b := Polygon{{2, 9}, {3, 7}, {9, 8}, {5, 11}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Xor(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArea := got.Area(); gotArea < 19.7 || gotArea > 20.3 {
		t.Errorf("Xor area %v outside expected band [19.7,20.3] (truth ~20.0; pre-fix bug was 0.25)", gotArea)
	}
}

func TestUnionSharedLocalMaxConfluence(t *testing.T) {
	// A and B both reach their local MAXIMUM at the shared vertex (8,6) — four
	// bounds (two per polygon) converging on one apex, with a cross-source
	// crossing just below at (8.33,3.33). The maxima pairing must pair each
	// polygon's own two bounds (vertex-identity, approximated by same source),
	// not the nearest coincident other-source edge; otherwise the hot ring
	// spanning both sources is never closed at the apex and everything above the
	// lower crossing is dropped. Pre-fix the engine returned 1.333 (≈ the
	// intersection area); the true union is ~5.67 (DESIGN.md §12.11, track C).
	a := Polygon{{9, 2}, {10, 4}, {8, 6}, {8, 4}}
	b := Polygon{{7, 0}, {8, 3}, {9, 4}, {8, 6}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	got, err := Union(MultiPolygon{{Outer: a}}, MultiPolygon{{Outer: b}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotArea := got.Area(); gotArea < 5.5 || gotArea > 5.85 {
		t.Errorf("Union area %v outside expected band [5.5,5.85] (truth ~5.67; pre-fix bug was 1.333)", gotArea)
	}
}

func TestSharedCollinearHorizontalEdge(t *testing.T) {
	// A triangle sitting on a wider triangle, sharing a partial collinear
	// HORIZONTAL edge with opposite interiors (A below the line y=2, B above),
	// plus a T-junction at (2,2) where B's left endpoint lands in A's top edge.
	// A = top edge horizontal (0,2)-(4,2), apex DOWN at (3,0), |A| = 4.
	// B = bottom edge horizontal (2,2)-(4,2) ⊂ A's top, apex UP at (3,4), |B| = 2.
	// They only touch along y=2 (no interior overlap), so Union/Xor = 6, Intersect
	// = 0, Difference = 4.
	//
	// Pre-fix the engine dropped B's entire above-edge region from the Union
	// (got 4.0 = just A). Root cause: A's local-MAX horizontal plateau was still
	// in the AEL when B's coincident local MIN was classified, because horizontal
	// maxima were flushed AFTER the scanline's local minima rather than before.
	// B's WindOther left-walk counted the closing plateau as if A continued above,
	// misclassifying B as non-contributing. The fix flushes top-reached
	// horizontals before classifying local minima, matching Clipper2's
	// DoTopOfScanbeam-before-next-InsertLocalMinima phasing (DESIGN.md §12.11).
	a := Polygon{{0, 2}, {4, 2}, {3, 0}}
	b := Polygon{{2, 2}, {4, 2}, {3, 4}}
	if !a.IsCCW() {
		a.Reverse()
	}
	if !b.IsCCW() {
		b.Reverse()
	}
	mpA := MultiPolygon{{Outer: a}}
	mpB := MultiPolygon{{Outer: b}}
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{"Union", func() (MultiPolygon, error) { return Union(mpA, mpB) }, 6},
		{"Intersect", func() (MultiPolygon, error) { return Intersect(mpA, mpB) }, 0},
		{"Difference", func() (MultiPolygon, error) { return Difference(mpA, mpB) }, 4},
		{"Xor", func() (MultiPolygon, error) { return Xor(mpA, mpB) }, 6},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if gotArea := got.Area(); math.Abs(gotArea-c.want) > 0.01 {
			t.Errorf("%s area %v want %v", c.name, gotArea, c.want)
		}
	}
}

func TestCoincidentHorizontalOverlapClosesRing(t *testing.T) {
	// Two simple quads whose top edges are collinear horizontals that PARTIALLY
	// overlap. SplitOverlaps resolves the overlap into a fully-coincident
	// different-source horizontal pair; the sweep then builds a cross-source
	// output ring whose two hot edges terminate at DIFFERENT points joined by a
	// horizontal top. closeBound's Case A/B handoff dropped one of those apex
	// vertices, collapsing the ring to a degenerate two-point sliver — so the
	// thin region was lost entirely (DESIGN.md §12.11). The fix emits the
	// closing edge's own apex in Case B when it differs from both ring ends.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	type check struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}
	// Repro 1: A's top (7,8)-(2,8) and B's top (6,8)-(1,8) overlap on x∈[2,6].
	// A lies almost entirely inside B; the difference is a thin top-right
	// triangle (6,6),(7,8),(6,8) of area ~0.99 that was dropped (got 0).
	a1 := mk(Polygon{{3, 4}, {6, 6}, {7, 8}, {2, 8}})
	b1 := mk(Polygon{{3, 0}, {6, 4}, {6, 8}, {1, 8}})
	// Repro 2: A's top (8,3)-(3,3) and B's mid edge (4,3)-(8,3) overlap on
	// x∈[4,8]. Union dropped B's whole upper region (got 6.75 vs ~21.74).
	a2 := mk(Polygon{{1, 2}, {7, 1}, {8, 3}, {3, 3}})
	b2 := mk(Polygon{{0, 2}, {4, 3}, {8, 3}, {2, 6}})
	checks := []check{
		{"r1/Union", func() (MultiPolygon, error) { return Union(a1, b1) }, 26.99},
		{"r1/Intersect", func() (MultiPolygon, error) { return Intersect(a1, b1) }, 11.0},
		{"r1/Difference", func() (MultiPolygon, error) { return Difference(a1, b1) }, 0.99},
		{"r1/Xor", func() (MultiPolygon, error) { return Xor(a1, b1) }, 15.99},
		{"r2/Union", func() (MultiPolygon, error) { return Union(a2, b2) }, 21.74},
		{"r2/Intersect", func() (MultiPolygon, error) { return Intersect(a2, b2) }, 0.25},
		{"r2/Difference", func() (MultiPolygon, error) { return Difference(a2, b2) }, 8.75},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.05 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestCoincidentHorizontalOppositeSideCancels(t *testing.T) {
	// Two quads sharing a collinear horizontal edge with their interiors on
	// OPPOSITE sides: A's bottom edge (4,4)-(5,4) (A above) coincides over x∈[4,5]
	// with B's top edge (5,4)-(2,4) (B below). The shared edge is interior to the
	// union and must cancel, with the two rings joined at the overlap endpoints
	// into a single outline.
	//
	// Pre-fix, [IntersectEdges] crossed the coincident pair: both-hot it fired
	// AddLocalMaxPoly (dropping A's continuation to apex (1,7) — Union got 6.0
	// vs 8.5); one-hot (Difference) it ran AddOutPt+SwapOutrecs, transferring A's
	// ring onto B's cold edge and collapsing it (got 0 vs 2.5). The fix detects a
	// coincident, opposite-interior (Seg.Reversed differs), positive-overlap
	// horizontal pair as a NON-crossing: it skips any ring-op and lets
	// [sweep.processHorzJoins] reconnect the two runs (DESIGN.md §12.11). Matches
	// Clipper2, which emits one ring [(1,7)(4,4)(2,4)(0,2)(3,2)(5,4)(7,3)].
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{4, 4}, {5, 4}, {7, 3}, {1, 7}})
	b := mk(Polygon{{0, 2}, {3, 2}, {5, 4}, {2, 4}})
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{"Union", func() (MultiPolygon, error) { return Union(a, b) }, 8.5},
		{"Intersect", func() (MultiPolygon, error) { return Intersect(a, b) }, 0},
		{"Difference", func() (MultiPolygon, error) { return Difference(a, b) }, 2.5},
		{"Xor", func() (MultiPolygon, error) { return Xor(a, b) }, 8.5},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.01 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
	// Union must be a single merged ring, not two touching rings.
	if got, _ := Union(a, b); len(got) != 1 {
		t.Errorf("Union: got %d rings, want 1 merged ring", len(got))
	}
}

func TestBooleanSharedVertexNotNested(t *testing.T) {
	// A and B are two simple quads that touch at EXACTLY one shared vertex
	// (12,8) and are otherwise disjoint — neither is inside the other. The sweep
	// emits both as correct CCW outer rings; the bug was in postprocess
	// (assembleResult's nested-outer demotion), which sampled the smaller ring's
	// vertex centroid and tested boundary-inclusive containment. Here A's
	// centroid lands exactly ON B's boundary edge, so Contains reported it inside
	// and A was wrongly demoted to a HOLE of B — collapsing Union from 54 to 18.
	// The fix samples a genuine interior point of the inner ring instead
	// (DESIGN.md §12.11, shared-vertex nesting).
	a := Polygon{{12, 8}, {9, 3}, {0, 6}, {9, 0}}  // |A| = 18
	b := Polygon{{8, 4}, {12, 8}, {7, 10}, {0, 8}} // |B| = 36
	mpA := MultiPolygon{{Outer: a}}
	mpB := MultiPolygon{{Outer: b}}

	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{"Union", func() (MultiPolygon, error) { return Union(mpA, mpB) }, 54},           // |A|+|B|, touch only
		{"Difference", func() (MultiPolygon, error) { return Difference(mpA, mpB) }, 18}, // = |A|
		{"Intersect", func() (MultiPolygon, error) { return Intersect(mpA, mpB) }, 0},
		{"Xor", func() (MultiPolygon, error) { return Xor(mpA, mpB) }, 54},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.5 {
			t.Errorf("%s area %v want %v (no false nesting at shared vertex)", c.name, got.Area(), c.want)
		}
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
