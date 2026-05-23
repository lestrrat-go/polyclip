package polyclip

import (
	"math"
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

// Operation labels reused across boolean test cases.
const (
	opUnion      = "Union"
	opIntersect  = "Intersect"
	opDifference = "Difference"
	opXor        = "Xor"
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

func TestInputOrientationNormalized(t *testing.T) {
	// The sweep derives WindDx from edge direction, assuming canonical input
	// orientation (CCW outer, CW hole). collectSegments normalizes either
	// winding, so a CW (clockwise) input must produce the same result as the
	// equivalent CCW input. Regression for the mixed-orientation undercount
	// (CW subject + CCW clip gave Union 26 instead of 28).
	ccwA := Polygon{{0, 0}, {4, 0}, {4, 4}, {0, 4}}
	ccwB := Polygon{{2, 2}, {6, 2}, {6, 6}, {2, 6}}
	rev := func(p Polygon) Polygon {
		out := make(Polygon, len(p))
		for i, pt := range p {
			out[len(p)-1-i] = pt
		}
		return out
	}
	want := struct{ u, i, d, x float64 }{28, 4, 12, 24}
	for _, tc := range []struct {
		name string
		a, b Polygon
	}{
		{"CCW/CCW", ccwA, ccwB},
		{"CW/CW", rev(ccwA), rev(ccwB)},
		{"CW/CCW", rev(ccwA), ccwB},
		{"CCW/CW", ccwA, rev(ccwB)},
	} {
		a := MultiPolygon{{Outer: tc.a}}
		b := MultiPolygon{{Outer: tc.b}}
		u, _ := Union(a, b)
		i, _ := Intersect(a, b)
		d, _ := Difference(a, b)
		x, _ := Xor(a, b)
		const tol = 0.01
		check := func(op string, got, exp float64) {
			if got < exp-tol || got > exp+tol {
				t.Errorf("%s %s = %.3f, want %.3f", tc.name, op, got, exp)
			}
		}
		check("Union", u.Area(), want.u)
		check("Intersect", i.Area(), want.i)
		check("Difference", d.Area(), want.d)
		check("Xor", x.Area(), want.x)
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
		{opUnion, func() (MultiPolygon, error) { return Union(mpA, mpB) }, 6},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(mpA, mpB) }, 0},
		{opDifference, func() (MultiPolygon, error) { return Difference(mpA, mpB) }, 4},
		{opXor, func() (MultiPolygon, error) { return Xor(mpA, mpB) }, 6},
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
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 8.5},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 2.5},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 8.5},
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

func TestCoincidentHorizontalExitReSpawns(t *testing.T) {
	// A's top horizontal (2,6)-(7,6) partially overlaps B's top horizontal
	// (3,6)-(0,6) over x∈[2,3], interiors opposite. SplitOverlaps fragments A's
	// horizontal into (2,6)-(3,6) + (3,6)-(7,6); at the overlap, B's bound ENDS
	// (its local-max plateau) but A's bound CONTINUES collinearly to (7,6). This
	// is a boundary EXIT, not a mutual cancellation: the opposite-side skip must
	// NOT fire, or A's right bound never re-spawns and A's whole upper body is
	// dropped (pre-fix Difference collapsed to 0.042 vs ~5.44). The discriminator
	// is continuesCollinearHorizontal (DESIGN.md §12.11).
	a := MultiPolygon{{Outer: Polygon{{4, 3}, {2, 6}, {7, 6}, {0, 8}}}}
	b := MultiPolygon{{Outer: Polygon{{5, 3}, {3, 6}, {0, 6}, {3, 4}}}}
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 10.441667},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0.558333},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 5.441667},
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
}

func TestCoincidentHorizontalCornerExitReSpawns(t *testing.T) {
	// A's top horizontal (7,6)-(8,6) partially overlaps B's top horizontal
	// (8,6)-(1,6) over x∈[7,8], interiors opposite. At the shared apex (8,6) B's
	// bound ENDS (its local-max plateau) but A's bound CONTINUES — turning
	// VERTICAL up to (8,8). Because the continuation is not horizontal,
	// continuesCollinearHorizontal does not catch it; the opposite-side skip
	// wrongly fired and A's upper region was dropped (pre-fix Difference 0.0 vs
	// 2.80). respawnHandoffAtOverlap detects the ending(hot)/continuing(cold)
	// handoff and falls through to the one-hot transfer so A re-spawns
	// (DESIGN.md §12.11). Xor is a separate, unrelated class and is not asserted.
	a := MultiPolygon{{Outer: Polygon{{7, 6}, {8, 6}, {8, 8}, {1, 3}}}}
	b := MultiPolygon{{Outer: Polygon{{1, 2}, {2, 0}, {8, 6}, {1, 6}}}}
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 25.8},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 2.7},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 2.8},
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
}

func TestCoincidentHorizontalBothContinueNoSkip(t *testing.T) {
	// A's bottom horizontal (6,5)-(8,5) and B's top horizontal (8,5)-(7,5)
	// overlap over x∈[7,8] at y=5, interiors opposite (A above, B below), and
	// share vertex (8,5). SplitOverlaps manufactures the coincident pair, but
	// here NEITHER bound terminates at the overlap: A continues up to (2,8) and
	// B continues up to (2,6), both with sloped edges. That is not a
	// doubled-boundary cancellation — the two horizontals are live boundaries
	// that each carry on. The opposite-side skip wrongly fired (sealing both
	// rings along y=5) and dropped everything above, collapsing Union 16.52→4.69
	// and Intersect 5.98→1.81. The skip now requires at least one IsBoundLast
	// (DESIGN.md §12.11). Difference/Xor were already correct (Xor never skips).
	a := MultiPolygon{{Outer: Polygon{{0, 3}, {6, 5}, {8, 5}, {2, 8}}}} // |A| = 16
	b := MultiPolygon{{Outer: Polygon{{0, 4}, {8, 5}, {7, 5}, {2, 6}}}} // |B| = 6.5
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 16.5228},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 5.9772},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 10.0228},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 10.5456},
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
}

func TestSharedVertexCollinearHorizontalSimplified(t *testing.T) {
	// A's top edge (0,5)-(3,5) is coincident with B's top edge along y=5, and
	// B carries a redundant collinear vertex (2,5) on that edge: (3,5),(2,5),(1,5)
	// are collinear. The shared apex (3,5) is B's local maximum AND A's
	// through-vertex. Before input simplification the extra collinear vertex made
	// the bound model treat (2,5) as a spurious turn, so a coincident-horizontal
	// piece was crossed mid-plateau via AddLocalMaxPoly, closing both rings early
	// — Union collapsed to 3.5 (< |A|). simplifyCollinearRing removes the
	// redundant vertex (a geometric no-op), and the sweep then handles the clean
	// shared-apex correctly. A and B only touch along y=5 (I = 0), so the exact
	// truth is U=|A|+|B|, D=|A|, X=|A|+|B| (DESIGN.md §12.11).
	a := MultiPolygon{{Outer: Polygon{{0, 0}, {0, 5}, {3, 5}, {-1, 6}}}} // |A| = 4
	b := MultiPolygon{{Outer: Polygon{{2, 5}, {1, 5}, {1, 1}, {3, 5}}}}  // |B| = 4, (2,5) collinear
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 8},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 4},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 8},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 1e-9 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
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
		{opUnion, func() (MultiPolygon, error) { return Union(mpA, mpB) }, 54},           // |A|+|B|, touch only
		{opDifference, func() (MultiPolygon, error) { return Difference(mpA, mpB) }, 18}, // = |A|
		{opIntersect, func() (MultiPolygon, error) { return Intersect(mpA, mpB) }, 0},
		{opXor, func() (MultiPolygon, error) { return Xor(mpA, mpB) }, 54},
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

func TestSharedVertexExitViaHorizontal(t *testing.T) {
	// B reaches its local MAX at the shared vertex (6,5); A passes THROUGH (6,5),
	// turning there onto its top horizontal (6,5)-(5,5) before continuing up to
	// its apex (1,7). A's local min (5,1) is interior to B, so A is cold below
	// (6,5) and must become the union boundary (hot) above it — the hand-off
	// dispatched at B's max by [sweep.handoffMaxThroughVertex].
	//
	// Pre-fix the hand-off rejected A's through-edge because A's cursor sat on
	// its horizontal at the confluence: the apex-column test used XAtY, which
	// returns a horizontal's Bot.X (5) rather than the through-vertex end (6),
	// so the candidate failed and A never went hot. A's whole upper region was
	// dropped (Union 16 vs 22.15, Difference 0 vs 6.17). The fix makes the
	// apex-column test horizontal-aware (DESIGN.md §12.11).
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{5, 1}, {6, 5}, {5, 5}, {1, 7}}) // |A| = 10
	b := mk(Polygon{{1, 1}, {7, 0}, {7, 3}, {6, 5}}) // |B| = 16
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 22.17},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 3.83},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 6.17},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 18.33},
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

func TestXorVertexOnEdgeSameSideTangle(t *testing.T) {
	// Two of A's vertices, (2,2) and (5,5), lie exactly on B's diagonal edge
	// (0,0)-(7,7). SplitTJunctions splits that edge at both, and the resulting
	// cross-source crossings reorder ring ownership so that at B's apex (7,7) the
	// two B bounds arrive on the SAME side (both back) of different rings — the
	// d50048a same-side AddLocalMaxPoly collision. The recovery used to reverse a
	// fixed edge (e2), which here was the already-correctly-oriented ring, leaving
	// a self-touching ring whose sub-loops cancel (Xor 13.2 vs truth 14.53).
	//
	// The fix re-derives front/back from the AEL order at the maximum (right bound
	// = FrontEdge in polyclip's mirror) and reverses the genuinely-inverted ring
	// (DESIGN.md §12.11). Validated against a Monte-Carlo area oracle, NOT
	// Clipper2 — Clipper2 is itself wrong on this tiny-integer degenerate input
	// (its Xor = 13.5). U/I/D were already correct and must stay so.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{2, 2}, {7, 2}, {5, 5}, {3, 5}})
	b := mk(Polygon{{0, 0}, {7, 7}, {3, 4}, {2, 6}})
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 16.767},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 2.233},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 8.267},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 14.533},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestXorVertexOnEdgeApexMerge(t *testing.T) {
	// Vertex-on-edge degeneracy where the bottom-up sweep builds two interleaved
	// rings that meet SAME-side at a maximum apex. The d50048a relabel+join
	// recovery folds the apex into a degenerate spike (the apex triangle's area
	// is lost): X-midvtx Xor was 8.94 vs truth 10.28, X-sharedvtx Xor 4.6 vs
	// 14.10. AddLocalMaxPoly now merges the two terminal rings as a figure-8
	// pinch at the apex and assembleResult's splitSelfTouchingRings decomposes
	// the self-touching walk into the correct simple rings (DESIGN.md §12.11).
	// Validated against a Monte-Carlo oracle (Xor == Union-Intersect), NOT
	// Clipper2, which rounds these fractional intersections at native scale.
	// U/I/D were already correct and must stay so.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	cases := []struct {
		name         string
		a, b         Polygon
		wantU, wantI float64
		wantD, wantX float64
	}{
		{
			// A's vertex (6,6) lies on B's edge (8,7)-(4,5); the apex triangle
			// (8,7),(6,6),(5.333,4.333) was collapsing to a spike.
			"midvtx",
			Polygon{{4, 1}, {6, 6}, {3, 6}, {0, 6}}, Polygon{{2, 4}, {3, 2}, {8, 7}, {4, 5}},
			16.3889, 6.1111, 8.8889, 10.2778,
		},
		{
			"sharedvtx",
			Polygon{{4, 1}, {7, 3}, {5, 4}, {4, 3}}, Polygon{{3, 4}, {5, 4}, {7, 2}, {8, 8}},
			14.8000, 0.7000, 3.8000, 14.1000,
		},
	}
	for _, c := range cases {
		a, b := mk(c.a), mk(c.b)
		checks := []struct {
			op   string
			run  func() (MultiPolygon, error)
			want float64
		}{
			{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, c.wantU},
			{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, c.wantI},
			{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, c.wantD},
			{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, c.wantX},
		}
		for _, ck := range checks {
			got, err := ck.run()
			if err != nil {
				t.Fatalf("%s/%s: unexpected error: %v", c.name, ck.op, err)
			}
			if math.Abs(got.Area()-ck.want) > 0.02 {
				t.Errorf("%s/%s area %v want %v", c.name, ck.op, got.Area(), ck.want)
			}
		}
	}
}

func TestUnionNotchTipOnHorizontalEdge(t *testing.T) {
	// A's vertex (6,5) is a concave-notch local maximum: A's two lower edges
	// (3,3)-(6,5) and (7,3)-(6,5) both end at (6,5), where A's source CLOSES a
	// maximum. B's horizontal edge (5,5)-(8,5) passes exactly through (6,5)
	// (SplitTJunctions splits it there). For Union, A's notch edges are hot, so
	// at the apex closeBound ran handoffMaxThroughVertex FIRST and saw B's cold
	// horizontal piece as a through-edge "continuing above" — it crossed A's hot
	// edge with it via IntersectEdges, whose SwapOutrecs moved A's ring onto the
	// horizontal, leaving A's edge cold so the genuine maximum (A's two edges)
	// never closed. The result shattered (Union 3.38 vs 14.29). I/D/X were
	// already correct because A's notch edges are not hot there.
	//
	// The fix: handoffMaxThroughVertex returns early when ae has a same-source
	// maxima partner at maxPt — a genuine maximum is closed by the maximaPartner/
	// resolveBetweenMaxima path, which crosses true between-edges itself; the
	// standalone hand-off is only for a vertex-on-edge EXIT with no such partner
	// (DESIGN.md §12.11). Validated against a Monte-Carlo area oracle.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{6, 5}, {7, 3}, {7, 8}, {3, 3}}) // |A| = 6
	b := mk(Polygon{{8, 5}, {2, 6}, {3, 0}, {5, 5}}) // |B| = 10
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 14.2879},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 1.7121},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 4.2879},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 12.5758},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestXorCoincidentMaxPlateauOverContinuingHorizontal(t *testing.T) {
	// B's local-max plateau (0,4)-(4,4) is coincident with A's MID-bound
	// horizontal (2,4)-(8,4) over x in [2,4] (A continues up to (8,4) then its
	// apex (0,8)). After SplitOverlaps the plateau is cut at (2,4); its piece
	// (2,4)-(4,4) tops out at (4,4). closeBound's plateauPartnerPending wrongly
	// DEFERRED that maximum because the coupled partner (A's continuing
	// horizontal) was still pending — but a continuing horizontal never closes
	// the deferred edge, so B's plateau edge stayed HOT in the AEL up to A's apex
	// (0,8), where it sat between A's two bounds and blocked maximaPartner from
	// pairing them. A's maximum then closed as two independent Case-A/B handoffs,
	// emitting two OVERLAPPING outer rings (membership correct, but Area()
	// double-counted the ~4 overlap): Xor was 25.37 vs truth 21.38.
	//
	// The fix: plateauPartnerPending defers only when the coupled horizontal
	// partner itself tops out at maxPt (a genuine shared-plateau max); if it
	// CONTINUES above maxPt the deferral is dropped so the maximum closes here
	// (DESIGN.md §12.11). Validated against a Monte-Carlo oracle (Xor == U-I),
	// NOT Clipper2. U/I/D were already correct and must stay so.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{0, 8}, {3, 0}, {2, 4}, {8, 4}}) // |A| = 14
	b := mk(Polygon{{0, 4}, {1, 1}, {5, 3}, {4, 4}}) // |B| = 9
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 22.1871},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0.8129},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 13.1871},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 21.3743},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestSharedVertexConcaveMaxThroughHorizontal(t *testing.T) {
	// A's vertex (7,6) is a through-vertex where A turns from the ascending edge
	// (3,3)-(7,6) onto its top HORIZONTAL (7,6)-(6,6). That same point is B's
	// concave-vertex local MAXIMUM: B's two edges (4,5)-(7,6) and (8,4)-(7,6) both
	// end there. A's through-edge sits AEL-between B's two max edges. Because A had
	// already advanced onto its horizontal, scanMaximaPartner's apex-column test
	// (XAtY, which returns a horizontal's Bot.X — the wrong end) bailed, so B's two
	// max edges never paired; resolveBetweenMaxima never crossed A's through-edge,
	// so A's WindOther stayed 0 (stale "outside B") as it entered B's interior
	// across the concave vertex. For Union/Difference (where contribution depends
	// on WindOther) A's body then dropped: U collapsed 13.22->2.66, D 11.22->0.78.
	// Intersect/Xor were correct (Xor ignores WindOther).
	//
	// The fix: scanMaximaPartner ADDITIONALLY accepts a horizontal between-edge
	// that spans the apex column (throughVertexOnColumn) AND whose bound continues
	// strictly above maxPt (boundContinuesAbove) — a genuine through-vertex. The
	// extra continues-above guard is essential: a horizontal that itself tops out
	// at the apex is part of a coincident max-plateau and must NOT widen pairing
	// (that broad form regressed coincident-horizontal cases). The clause is purely
	// additive so it never removes a previously-paired confluence (DESIGN.md
	// §12.11). Validated against a Monte-Carlo area oracle (all of U=X+I, D=|A|-I,
	// X=U-I hold), NOT Clipper2.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	cases := []struct {
		name       string
		a, b       Polygon
		u, i, d, x float64
	}{
		{
			"family1",
			Polygon{{3, 3}, {7, 6}, {6, 6}, {1, 8}}, // |A| = 12
			Polygon{{7, 7}, {4, 5}, {7, 6}, {8, 4}}, // |B| = 2, shared vtx (7,6)
			13.2188, 0.7812, 11.2188, 12.4375,
		},
		{
			"family2",
			Polygon{{6, 5}, {3, 4}, {1, 7}, {4, 0}}, // |A| = 9
			Polygon{{4, 5}, {0, 7}, {3, 2}, {6, 5}}, // |B| = 10, shared vtx (6,5)
			14.4068, 4.5932, 4.4068, 9.8136,
		},
	}
	for _, tc := range cases {
		a, b := mk(tc.a), mk(tc.b)
		checks := []struct {
			name string
			run  func() (MultiPolygon, error)
			want float64
		}{
			{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, tc.u},
			{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, tc.i},
			{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, tc.d},
			{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, tc.x},
		}
		for _, c := range checks {
			got, err := c.run()
			if err != nil {
				t.Fatalf("%s/%s: unexpected error: %v", tc.name, c.name, err)
			}
			if math.Abs(got.Area()-c.want) > 0.02 {
				t.Errorf("%s/%s area %v want %v", tc.name, c.name, got.Area(), c.want)
			}
		}
	}
}

func TestIntersectVertexOnEdgeSelfClose(t *testing.T) {
	// B's vertices (2,2) and (3,3) lie on A's diagonal edge y=x. The bound that
	// maxes out at a shared through-vertex must close its cross-source ring when
	// the coupled (continuing) bound becomes non-contributing above the vertex;
	// otherwise the continuing edge drags a spurious sub-loop in and the
	// Intersect region cancels (was 2.97 vs truth 1.47). Validated vs a
	// Monte-Carlo oracle (DESIGN.md §12.11). U/D/X already correct.
	mk := func(p Polygon) MultiPolygon {
		if !p.IsCCW() {
			p.Reverse()
		}
		return MultiPolygon{{Outer: p}}
	}
	a := mk(Polygon{{0, 0}, {4, 4}, {8, 5}, {1, 8}})
	b := mk(Polygon{{2, 2}, {5, 2}, {3, 3}, {0, 3}})
	checks := []struct {
		op   string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 25.0331},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 1.4669},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 22.0331},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 23.5662},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.op, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.op, got.Area(), c.want)
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

func TestSplitOverlapsCollinearSpikeNoHang(t *testing.T) {
	// A is a "spike" quad: (-10,-10),(67,67),(10,10) are collinear on y=x, so
	// edge (-10,-10)-(67,67) and edge (67,67)-(10,10) overlap collinearly. The
	// overlap's lower endpoint is exactly the vertex (10,10) after snapping, but
	// collinearOverlap used to RE-PROJECT it via xAtY, which rounded it a few
	// fixed-point units off the shared line. SplitOverlaps then cut at the
	// off-line point, manufacturing a spurious tiny horizontal sliver it could
	// never resolve -> the fixed-point loop spun forever, growing the segment
	// slice without bound (a CI-wedging infinite loop / OOM). The fix takes the
	// overlap endpoints directly from the exact input endpoints. Originally
	// FuzzDifference/a84b6e584bd0aa6b. Guard: the op merely completes (the
	// package test timeout catches a regression hang) and stays area-bounded.
	a := MultiPolygon{ExPolygon{Outer: Polygon{
		{-10, -10}, {67, 67}, {10, 10}, {-99, 10},
	}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{
		{-3, -3}, {3, -3}, {3, 76}, {-3, 3},
	}}}
	for _, tc := range []struct {
		name string
		fn   func(MultiPolygon, MultiPolygon) (MultiPolygon, error)
	}{
		{opUnion, Union},
		{opIntersect, Intersect},
		{opDifference, Difference},
		{opXor, Xor},
	} {
		got, err := tc.fn(a, b)
		if err != nil && err != ErrHorizontalNotSupported {
			t.Fatalf("%s: unexpected error: %v", tc.name, err)
		}
		if err == nil && got.Area() < -1e-6 {
			t.Errorf("%s: negative area %g", tc.name, got.Area())
		}
	}
}

func TestCollinearMidVertexSimplified(t *testing.T) {
	// A ring with a vertex lying exactly on the straight line between its
	// neighbours (a redundant collinear-through vertex) mis-built its output
	// rings: the bound model treats the extra segment's shared endpoint as a
	// turn/maximum of its bound. When that collinear vertex sat on a flat-top
	// max plateau whose end touched the other polygon's edge (a vertex-on-edge
	// confluence), the sweep produced overlapping or dropped rings.
	//
	//   - Union seed FuzzUnion/a61d9e1c973bf217: B's top is (-7,5),(-19,5),(-33,5)
	//     — (-19,5) is collinear on y=5. Union under-counted 369.50 (truth ~563)
	//     and Difference 213.50 (truth ~407); A's body dropped.
	//   - Xor seed FuzzXor/28f7e421c4b5068b: A's top is (5,5),(-5,5),(-14,5) —
	//     (-5,5) is collinear on y=5, and A's plateau corner (5,5) lies on B's
	//     vertical edge x=5. Xor over-counted 4182.50 (truth ~2522) as two
	//     overlapping outer rings.
	//
	// The fix removes collinear-through vertices from each input ring before the
	// sweep (boolean.go appendRing / simplifyCollinearRing) — an exact geometric
	// no-op for a simple polygon. Areas validated against a Monte-Carlo oracle
	// (and the identities U=I+X, D=|A|-I, X=U-I all hold), NOT Clipper2. The
	// expected values are the now-correct engine outputs.
	cases := []struct {
		name       string
		a, b       MultiPolygon
		u, i, d, x float64
	}{
		{
			"union-seed",
			makeQuad(143, -5, 5, 7, 9, 5, -60, 5),
			makeQuad(-7, 5, -19, 5, -33, 5, 5, -7),
			563.502298, 51.497702, 407.502298, 512.004597,
		},
		{
			"xor-seed",
			makeQuad(-14, 5, 97, -70, 5, 5, -5, 5),
			makeQuad(5, -34, 15, 49, 105, 107, 5, 73),
			2572.196625, 45.303375, 667.196625, 2526.893249,
		},
	}
	for _, tc := range cases {
		checks := []struct {
			name string
			run  func() (MultiPolygon, error)
			want float64
		}{
			{opUnion, func() (MultiPolygon, error) { return Union(tc.a, tc.b) }, tc.u},
			{opIntersect, func() (MultiPolygon, error) { return Intersect(tc.a, tc.b) }, tc.i},
			{opDifference, func() (MultiPolygon, error) { return Difference(tc.a, tc.b) }, tc.d},
			{opXor, func() (MultiPolygon, error) { return Xor(tc.a, tc.b) }, tc.x},
		}
		for _, c := range checks {
			got, err := c.run()
			if err != nil {
				t.Fatalf("%s/%s: unexpected error: %v", tc.name, c.name, err)
			}
			if math.Abs(got.Area()-c.want) > 0.02 {
				t.Errorf("%s/%s area %v want %v", tc.name, c.name, got.Area(), c.want)
			}
		}
	}
}

func TestCrossingSnapsOrderIndependently(t *testing.T) {
	// Seed FuzzIntersect/ff9aee9b909462b0. A's bottom edge (-10,-10)-(48,48)
	// lies on y=x and crosses B's vertical edge x=3 at the lattice point (3,3).
	// The proper-crossing point was computed by parametrising along the FIRST
	// argument of properIntersection, so the same geometric crossing rounded to
	// two grid points one unit apart depending on argument order. doIntersections
	// computed that crossing in two adjacent beams with the edges in swapped AEL
	// order (the crossing itself swaps them); the second value escaped the
	// already-handled "<= botY" beam guard, so the crossing was dispatched twice
	// and the second dispatch undid the first — leaving A's diagonal on the wrong
	// ring above (3,3). Union collapsed 2516.89->147.86 and Intersect bloated
	// 351.11->1380.49 (X was unaffected because it ignores WindOther). Ordering
	// the two segments canonically in properIntersection makes the rounded point
	// independent of caller order, so the guard reliably suppresses the second
	// dispatch (DESIGN.md §12.11). Areas validated against a Monte-Carlo oracle
	// (U=I+X, D=|A|-I, X=U-I all hold), NOT Clipper2.
	a := makeQuad(-10, -10, 48, 48, 10, 40, -10, 65)
	b := makeQuad(-3, 53, 3, -114, 3, 99, -36, 3)
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 2516.885291},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 351.114709},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 1268.885291},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 2165.770582},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestTouchingAlongHorizontalEdgeNotNested(t *testing.T) {
	// Two polygons that touch only along a full shared horizontal edge — A
	// below, B above — are disjoint-but-adjacent (I=0), so Union/Xor must
	// emit two sibling outer rings, not nest one inside the other. The sweep
	// emits B's ring CW (typical of the non-Union FrontEdge convention); the
	// hole→outer promotion then reverses it IF it has no enclosing outer. That
	// hinges on interiorPoint sampling B's open interior. When B's mean Y
	// equals the shared edge's Y, the scanline ran along the shared horizontal
	// and returned a boundary point that Polygon.Contains read as inside A,
	// wrongly nesting B as a hole and collapsing Union 43->19 (DESIGN.md
	// §12.11). interiorPoint now samples strictly between distinct vertex Ys.
	a := makeQuad(8, 1, 12, 9, 6, 9, 5, 6)
	b := makeQuad(6, 9, 12, 9, 2, 11, 4, 7)
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 43},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 31},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 43},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestInteriorPointAvoidsHorizontalEdge(t *testing.T) {
	// interiorPoint must return a strictly-interior point even when the
	// vertex-mean Y coincides with a horizontal edge. For this quad the mean
	// Y is 9 — exactly the (6,9)-(12,9) top edge — and the old code returned
	// (7.5,9), a point ON that edge.
	p := Polygon{{12, 9}, {6, 9}, {4, 7}, {2, 11}}
	pt, ok := interiorPoint(p)
	if !ok {
		t.Fatal("interiorPoint returned !ok for a valid quad")
	}
	if pointOnRingBoundary(p, pt) {
		t.Errorf("interiorPoint %v lies on the ring boundary", pt)
	}
	if !p.Contains(pt) {
		t.Errorf("interiorPoint %v not inside its own ring", pt)
	}
}

func TestXorCoincidentPlateauKeepsApex(t *testing.T) {
	// Two polygons whose top plateaus overlap and share the right apex (12,9):
	// A's top (12,9)-(7,9)-(5,9) and B's top (12,9)-(10,9) coincide over
	// [10,12]@y=9. The overlap is a doubled boundary (both interiors below),
	// not a transversal crossing. branchBothHot's tunnel branch treated the
	// coincident plateau pieces as a point-crossing, joining the intersection
	// ring into the union ring and respawning a degenerate apex spike — the
	// Xor hole lost its (12,9) apex triangle and over-counted (21.15 vs 16.60).
	// Coincident horizontal hot edges now interleave instead (DESIGN.md
	// §12.11). Values validated against a Monte-Carlo oracle (and U=I+X,
	// D=|A|-I, X=U-I all hold), NOT Clipper2.
	a := makeQuad(5, 9, 3, 3, 12, 9, 7, 9)
	b := makeQuad(12, 9, 10, 9, 2, 10, 6, 3)
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 34.801471},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 18.198529},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 2.801471},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 16.602941},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestXorVertexOnEdgeApexKeepsCorner(t *testing.T) {
	// B's plateau apex (9.5,7) — where B's horizontal top meets its right edge —
	// lies exactly ON A's edge (12,8)-(7,6) (slope 0.4 passes through it). The
	// Xor corner of A right of B, the triangle (12,8)-(9.5,7)-(8.255,5.617), was
	// dropped: at A's apex (12,8) the triangle ring (terminal) and the larger A
	// ring (still building up its left edge to (5,10)) meet SAME-side, and
	// AddLocalMaxPoly's relabel+JoinOutrecPaths folded the apex into a degenerate
	// out-and-back spike, losing the corner (X 15.46 vs 16.57, idX violated).
	// AddLocalMaxPoly now splices the terminal loop into the continuing ring as a
	// self-touching detour, preserving the continuing back edge's tip (DESIGN.md
	// §12.11). Values validated against a Monte-Carlo oracle (U=I+X, D=|A|-I,
	// X=U-I all hold), NOT Clipper2.
	a := MultiPolygon{ExPolygon{Outer: Polygon{{X: 1, Y: 1}, {X: 12, Y: 8}, {X: 7, Y: 6}, {X: 5, Y: 10}}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{X: 9.5, Y: 7}, {X: 8, Y: 7}, {X: 3, Y: 7}, {X: 5, Y: 2}}}}
	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 28.159420},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 11.590580},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 11.909420},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 16.568841},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}
}

func TestSimplifyCollinearRing(t *testing.T) {
	// Unit-level: collinear-through vertices removed, real corners and rings
	// with a repeated vertex preserved.
	p := func(x, y int64) fixed.Point { return fixed.Point{X: fixed.Coord(x), Y: fixed.Coord(y)} }
	cases := []struct {
		name string
		in   []fixed.Point
		want int
	}{
		{"square no collinear", []fixed.Point{p(0, 0), p(4, 0), p(4, 4), p(0, 4)}, 4},
		{"one collinear on edge", []fixed.Point{p(0, 0), p(2, 0), p(4, 0), p(4, 4), p(0, 4)}, 4},
		{"run of collinear", []fixed.Point{p(0, 0), p(1, 0), p(2, 0), p(3, 0), p(4, 0), p(4, 4), p(0, 4)}, 4},
		{"collinear at wrap", []fixed.Point{p(2, 0), p(4, 0), p(4, 4), p(0, 4), p(0, 0)}, 4},
		{"triangle", []fixed.Point{p(0, 0), p(4, 0), p(2, 4)}, 3},
	}
	for _, c := range cases {
		got := simplifyCollinearRing(c.in)
		if len(got) != c.want {
			t.Errorf("%s: got %d verts %v, want %d", c.name, len(got), got, c.want)
		}
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

func TestBooleanNearScanlineCrossingNoDoubleDispatch(t *testing.T) {
	// Eight fuzz-discovered inputs (simple non-convex quads, large coords) where
	// an edge crossing rounds to y+ε just past a scanbeam top. doIntersections
	// correctly defers it to the next beam, but reconcileSharedVertexCrossings —
	// seeing the two edges share a rounded CurrX at the scanline — also crossed
	// it, double-dispatching the same crossing as a phantom local-min then the
	// real crossing. That opened+closed a zero-area ring and dropped a whole
	// region (Union collapsing below max(a,b), Difference/Xor exceeding their
	// bounds). reconcile now skips a pair that genuinely ProperCrosses above y.
	// Each input is checked against the op's area invariant (the bound the fuzz
	// corpus reported violated).
	q := func(c ...float64) MultiPolygon {
		return MultiPolygon{ExPolygon{Outer: Polygon{
			{X: c[0], Y: c[1]}, {X: c[2], Y: c[3]}, {X: c[4], Y: c[5]}, {X: c[6], Y: c[7]},
		}}}
	}
	cases := []struct {
		name string
		a, b MultiPolygon
	}{
		{"U-3e7c", q(85, 25, -59, 88, -65, 5, -5, 60), q(5, 5, 15, 5, 15, 15, 5, 83)},
		{"U-4ca9", q(-5, 2, 61, 8, -31, 50, 24, -66), q(-1, 29, 15, -75, 15, 124, -31, 74)},
		{"I-ad5d", q(-93, 144, 5, -117, 5, 86, -5, -8), q(-115, 77, 25, -20, 25, 5, 15, -1)},
		{"I-c5e2", q(-10, -10, 10, -10, 10, -10, -10, 10), q(-42, -3, 3, -28, 3, 50, -3, 3)},
		{"D-43f7", q(5, 59, 5, 82, 5, -16, 146, -57), q(-69, 25, 116, -25, 25, 5, 38, 124)},
		{"D-b3c6", q(5, 59, 5, 82, 5, -16, 146, -57), q(-69, 25, 116, -25, 25, 5, 38, 103)},
		{"X-279f", q(30, -56, 10, 47, -51, 10, -6, 16), q(-3, -5, -2, 32, 70, 3, -3, 82)},
		{"X-9614", q(30, -56, 10, 47, -51, 10, -6, 16), q(-3, -18, 51, 118, 3, 3, -3, 82)},
	}
	const eps = 1e-6
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			aA, bA := tc.a.Area(), tc.b.Area()
			u, err := Union(tc.a, tc.b)
			if err != nil {
				t.Fatalf("union: %v", err)
			}
			i, _ := Intersect(tc.a, tc.b)
			d, _ := Difference(tc.a, tc.b)
			x, _ := Xor(tc.a, tc.b)
			uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
			// Per-op area invariants (the fuzz bounds the corpus violated).
			lo := math.Max(aA, bA)
			if uA < lo-eps || uA > aA+bA+eps {
				t.Errorf("Union %g not in [max(a,b)=%g, a+b=%g]", uA, lo, aA+bA)
			}
			if iA > math.Min(aA, bA)+eps || iA < -eps {
				t.Errorf("Intersect %g not in [0, min(a,b)=%g]", iA, math.Min(aA, bA))
			}
			if dA > aA+eps || dA < -eps {
				t.Errorf("Difference %g not in [0, a=%g]", dA, aA)
			}
			if xA > aA+bA+eps || xA < -eps {
				t.Errorf("Xor %g not in [0, a+b=%g]", xA, aA+bA)
			}
			// Noise-free set identities.
			if math.Abs(uA-(aA+bA-iA)) > 0.02 {
				t.Errorf("U=A+B-I violated: U=%g A+B-I=%g", uA, aA+bA-iA)
			}
			if math.Abs(dA-(aA-iA)) > 0.02 {
				t.Errorf("D=A-I violated: D=%g A-I=%g", dA, aA-iA)
			}
			if math.Abs(xA-(uA-iA)) > 0.02 {
				t.Errorf("X=U-I violated: X=%g U-I=%g", xA, uA-iA)
			}
		})
	}
}

func TestBooleanInputHoleIslandNesting(t *testing.T) {
	// A is a 10x10 square with a centered 6x6 hole (area 64). B is a 2x2 square
	// entirely inside that hole (area 4). The union's three boundary rings are
	// the square (CCW, depth 0 -> filled), the 6x6 hole (CW, depth 1 -> hole of
	// the square), and B (CCW, depth 2 -> a filled ISLAND that sits in the hole,
	// hence its own top-level ExPolygon, not a hole). assembleResult computed
	// nesting depth among outer rings only, so it saw B as directly inside the
	// square (depth 1) and wrongly demoted it to a hole, dropping the real 6x6
	// hole (Union/Xor 96 instead of 68). It now builds the containment forest
	// over ALL rings (DESIGN.md §11.9). Values are exact (axis-aligned).
	a := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes: []Polygon{{{X: 2, Y: 2}, {X: 2, Y: 8}, {X: 8, Y: 8}, {X: 8, Y: 2}}},
	}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{X: 4, Y: 4}, {X: 6, Y: 4}, {X: 6, Y: 6}, {X: 4, Y: 6}}}}

	checks := []struct {
		name string
		run  func() (MultiPolygon, error)
		want float64
	}{
		{opUnion, func() (MultiPolygon, error) { return Union(a, b) }, 68},
		{opIntersect, func() (MultiPolygon, error) { return Intersect(a, b) }, 0},
		{opDifference, func() (MultiPolygon, error) { return Difference(a, b) }, 64},
		{opXor, func() (MultiPolygon, error) { return Xor(a, b) }, 68},
	}
	for _, c := range checks {
		got, err := c.run()
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if math.Abs(got.Area()-c.want) > 0.02 {
			t.Errorf("%s area %v want %v", c.name, got.Area(), c.want)
		}
	}

	// The union must keep the island as a SEPARATE top-level piece, and the
	// square must keep its 6x6 hole — exactly two pieces, one holed, one not.
	u, err := Union(a, b)
	if err != nil {
		t.Fatalf("union: %v", err)
	}
	if len(u) != 2 {
		t.Fatalf("union pieces = %d, want 2 (square+hole, island)", len(u))
	}
	holed, island := 0, 0
	for _, ex := range u {
		switch len(ex.Holes) {
		case 1:
			holed++
		case 0:
			island++
		}
	}
	if holed != 1 || island != 1 {
		t.Errorf("union pieces: holed=%d island=%d, want 1 and 1", holed, island)
	}
}

func TestBooleanHoledInputCoincidentPlateau(t *testing.T) {
	// A is a 12x12 square with a triangular hole (3,3)-(3,9)-(9,9) whose top
	// edge is a horizontal at y=9. B is a quad whose own top edge is also a
	// horizontal at y=9 that partially overlaps the hole's top, so B's local-max
	// plateau and the hole's local-max plateau are coincident over x in [3,4].
	//
	// The hole's top plateau is split by B's vertex at (4,9) into T-junction
	// fragments and is traversed past (4,9) to its true apex at (3,9). closeBound
	// wrongly deferred B's coinciding max edge to that partner plateau (matching
	// only the current fragment's far X), but the partner passes THROUGH (4,9)
	// and closes its own subject ring at (3,9) — B's clip edge was never closed
	// and lingered hot in the AEL, where the square's top horizontal later
	// crossed it and dropped the whole upper-right region (Difference 57.96
	// instead of 125.46). plateauPartnerPending now defers only when the partner
	// truly tops out at the apex, or borders the other source there (DESIGN.md
	// §12.11). The four set identities must hold to within MC/grid tolerance.
	a := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 12, Y: 0}, {X: 12, Y: 12}, {X: 0, Y: 12}},
		Holes: []Polygon{{{X: 9, Y: 9}, {X: 3, Y: 3}, {X: 3, Y: 9}, {X: 7, Y: 9}}},
	}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{X: 4, Y: 9}, {X: 2, Y: 9}, {X: 4, Y: 8}, {X: 10, Y: 8}}}}

	u, err := Union(a, b)
	if err != nil {
		t.Fatalf("union: %v", err)
	}
	i, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("intersect: %v", err)
	}
	d, err := Difference(a, b)
	if err != nil {
		t.Fatalf("difference: %v", err)
	}
	x, err := Xor(a, b)
	if err != nil {
		t.Fatalf("xor: %v", err)
	}
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()

	// Difference must not have collapsed (the bug dropped ~68 of 125.5).
	if dA < 120 {
		t.Errorf("difference area %v collapsed (want ~%v)", dA, aA-iA)
	}
	// Noise-free set identities: U=A+B-I, D=A-I, X=U-I.
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"U=A+B-I", uA, aA + bA - iA},
		{"D=A-I", dA, aA - iA},
		{"X=U-I", xA, uA - iA},
	} {
		if math.Abs(c.got-c.want) > 0.02 {
			t.Errorf("%s: got %v want %v", c.name, c.got, c.want)
		}
	}
}

func TestBooleanHoledInputFlatHoleTopThroughClip(t *testing.T) {
	// A is a 12x12 square with a quad hole whose TOP edge is a horizontal at y=7
	// ((3,7)-(6,7)). B is a quad fully inside the square that overlaps the hole,
	// so the hole pokes out of B on the left and B's clip edge crosses the hole's
	// flat top. The difference region rides the hole's left bound up to the hole
	// apex (3,7); because the apex is the LEFT end of the hole's top plateau, the
	// hole's right bound reaches (3,7) only after traversing that horizontal,
	// which closeBound had not yet seen — so it closed the region prematurely
	// (Case A) and the plateau, crossing B's edge, fragmented the ring and dropped
	// ~64 of the 82.3 area (Difference 18.30). plateauMaxPartnerPending now defers
	// to the geometric same-source maxima partner so doHorizontal's own close
	// pairs the two and joins the rings (DESIGN.md §12.11). Tilting the hole top
	// off-horizontal already worked; this asserts the flat-top variant matches.
	a := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 12, Y: 0}, {X: 12, Y: 12}, {X: 0, Y: 12}},
		Holes: []Polygon{{{X: 8, Y: 4}, {X: 5, Y: 5}, {X: 3, Y: 7}, {X: 6, Y: 7}}},
	}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{X: 11, Y: 4}, {X: 7, Y: 12}, {X: 0, Y: 1}, {X: 4, Y: 0}}}}

	u, err := Union(a, b)
	if err != nil {
		t.Fatalf("union: %v", err)
	}
	i, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("intersect: %v", err)
	}
	d, err := Difference(a, b)
	if err != nil {
		t.Fatalf("difference: %v", err)
	}
	x, err := Xor(a, b)
	if err != nil {
		t.Fatalf("xor: %v", err)
	}
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()

	// Difference must not have collapsed (the bug dropped ~64 of ~82.3).
	if dA < 78 {
		t.Errorf("difference area %v collapsed (want ~%v)", dA, aA-iA)
	}
	// Noise-free set identities: U=A+B-I, D=A-I, X=U-I.
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{"U=A+B-I", uA, aA + bA - iA},
		{"D=A-I", dA, aA - iA},
		{"X=U-I", xA, uA - iA},
	} {
		if math.Abs(c.got-c.want) > 0.02 {
			t.Errorf("%s: got %v want %v", c.name, c.got, c.want)
		}
	}
}

func TestBooleanDifferenceIdenticalRotatedCancels(t *testing.T) {
	// A and B are the SAME quad with vertices rotated by one position, so the
	// mpolyEqual idempotency short-circuit (which compares vertex order) does
	// NOT fire and the engine runs. The sweep emits the region twice — once CCW
	// and once CW (coincident boundaries) — which must cancel to zero area.
	// assembleResult's containment forest treats two equal-area coincident
	// rings as outer+hole via an orientation tie-break (DESIGN.md §11.9); a
	// strict larger-area rule alone left both as filled outers (area doubled).
	a := MultiPolygon{ExPolygon{Outer: Polygon{{X: 7, Y: 11}, {X: 7, Y: 8}, {X: 5, Y: 3}, {X: 12, Y: 2}}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{X: 7, Y: 8}, {X: 5, Y: 3}, {X: 12, Y: 2}, {X: 7, Y: 11}}}}
	d, err := Difference(a, b)
	if err != nil {
		t.Fatalf("difference: %v", err)
	}
	if d.Area() > 0.02 {
		t.Errorf("Difference area %v want 0", d.Area())
	}
	x, err := Xor(a, b)
	if err != nil {
		t.Fatalf("xor: %v", err)
	}
	if x.Area() > 0.02 {
		t.Errorf("Xor area %v want 0", x.Area())
	}
}
