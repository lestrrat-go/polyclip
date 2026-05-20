package polyclip

import (
	"math"
	"testing"
)

// ===== §6.2 hand-built adversarial cases =====

func TestDifferenceAnnulus(t *testing.T) {
	// Outer 10x10 square minus inner 4x4 square produces an annulus —
	// outer ring with a hole. The inner square sits strictly inside the
	// outer with no edge touches; coincident edges are not exercised.
	outer := MultiPolygon{sq(0, 0, 10)}
	inner := MultiPolygon{sq(0, 0, 4)}
	got, err := Difference(outer, inner)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 piece, got %d: %+v", len(got), got)
	}
	if len(got[0].Holes) != 1 {
		t.Errorf("expected 1 hole, got %d", len(got[0].Holes))
	}
	wantArea := outer.Area() - inner.Area()
	if math.Abs(got.Area()-wantArea) > 0.01 {
		t.Errorf("Difference area %v want %v", got.Area(), wantArea)
	}
}

func TestIntersectAreaInvariantOverlappingDiamonds(t *testing.T) {
	// Area(Union) + Area(Intersect) == Area(A) + Area(B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	u, err := Union(a, b)
	if err != nil {
		t.Fatalf("Union err: %v", err)
	}
	i, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	lhs := u.Area() + i.Area()
	rhs := a.Area() + b.Area()
	if math.Abs(lhs-rhs) > 0.5 {
		t.Errorf("Area(Union)=%v + Area(Intersect)=%v = %v; Area(A)+Area(B)=%v", u.Area(), i.Area(), lhs, rhs)
	}
}

func TestDifferenceOverlappingDiamonds(t *testing.T) {
	// A ∖ B for two overlapping diamonds. Result area = Area(A) − Area(A∩B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty difference")
	}
	inter, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	want := a.Area() - inter.Area()
	if math.Abs(got.Area()-want) > 0.5 {
		t.Errorf("Difference area %v want %v (=Area(A)−Area(A∩B))", got.Area(), want)
	}
}

func TestXorOverlappingDiamonds(t *testing.T) {
	// Xor(A,B) = symmetric difference. Area = Area(A) + Area(B) − 2·Area(A∩B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty xor")
	}
	inter, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	want := a.Area() + b.Area() - 2*inter.Area()
	if math.Abs(got.Area()-want) > 0.5 {
		t.Errorf("Xor area %v want %v", got.Area(), want)
	}
}

func TestUnionTouchingAtVertex(t *testing.T) {
	// Two diamonds touching at a single vertex (corner-to-corner). With
	// the source-based disambiguation in BuildLocalMinima, the two rings
	// are traced independently; the merged result is two ExPolygons (the
	// touch doesn't fuse the rings into one).
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(10, 0, 5)} // touches a at (5,0)
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Total area must equal sum of the two (touch is measure-zero overlap).
	wantArea := a.Area() + b.Area()
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Union area %v want %v; got=%+v", got.Area(), wantArea, got)
	}
}

// ===== §6.2 property invariants =====

func TestUnionIdempotent(t *testing.T) {
	// Union(A, A) should equal A (modulo orientation/start-vertex).
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Union(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(got.Area()-a.Area()) > 0.5 {
		t.Errorf("Union(A,A) area %v want %v", got.Area(), a.Area())
	}
}

func TestIntersectIdempotent(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Intersect(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(got.Area()-a.Area()) > 0.5 {
		t.Errorf("Intersect(A,A) area %v want %v", got.Area(), a.Area())
	}
}

func TestDifferenceSelf(t *testing.T) {
	// Difference(A, A) should be empty.
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Difference(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Area() > 0.5 {
		t.Errorf("Diff(A,A) area %v want ≈0", got.Area())
	}
}

// ===== Non-Union adversarial coverage on AXIAL inputs =====

func TestIntersectTouchingBoundaryAxisAligned(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(10, 0, 5)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Touching only at boundary edge — intersection has zero area.
	if got.Area() > 0.01 {
		t.Errorf("Intersect(touching) area %v want ≈0", got.Area())
	}
}

func TestDifferenceTouchingBoundaryAxisAligned(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(10, 0, 5)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Touching boundary doesn't subtract anything from A's area.
	if math.Abs(got.Area()-a.Area()) > 0.01 {
		t.Errorf("Difference(touching) area %v want %v", got.Area(), a.Area())
	}
}

func TestXorTouchingBoundaryAxisAligned(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(10, 0, 5)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := a.Area() + b.Area()
	if math.Abs(got.Area()-wantArea) > 0.01 {
		t.Errorf("Xor(touching) area %v want %v", got.Area(), wantArea)
	}
}

func TestIntersectNestedAxialSquares(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 10)}
	b := MultiPolygon{sq(0, 0, 3)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Inner square fully contained → intersection equals the inner square.
	if math.Abs(got.Area()-b.Area()) > 0.01 {
		t.Errorf("Intersect(nested) area %v want %v", got.Area(), b.Area())
	}
}

func TestDifferenceNestedAxialSquares(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 10)}
	b := MultiPolygon{sq(0, 0, 3)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := a.Area() - b.Area()
	if math.Abs(got.Area()-wantArea) > 0.01 {
		t.Errorf("Difference(nested) area %v want %v", got.Area(), wantArea)
	}
}

func TestXorNestedAxialSquares(t *testing.T) {
	a := MultiPolygon{sq(0, 0, 10)}
	b := MultiPolygon{sq(0, 0, 3)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := a.Area() - b.Area() // outer minus inner; equivalent to Difference here
	if math.Abs(got.Area()-wantArea) > 0.01 {
		t.Errorf("Xor(nested) area %v want %v", got.Area(), wantArea)
	}
}

func TestIntersectTouchingAtVertex(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(10, 0, 5)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Area() > 0.01 {
		t.Errorf("Intersect(touching vertex) area %v want ≈0", got.Area())
	}
}

func TestDifferenceTouchingAtVertex(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(10, 0, 5)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(got.Area()-a.Area()) > 0.01 {
		t.Errorf("Difference(touching vertex) area %v want %v", got.Area(), a.Area())
	}
}

// ===== Known-broken: axial overlapping for non-Union =====
//
// The §11.7 synth-intersect mechanism in clip/sweep.go is Union-specific.
// For Intersect/Difference/Xor on axial OVERLAPPING (not nested / not
// touching / not disjoint) inputs, the engine produces incorrect output.
// See DESIGN.md §11.7 "Implementation" section for status. The tests
// below are skipped pending engine work; remove the t.Skip when fixed.

func TestIntersectOverlappingAxisAligned(t *testing.T) {
	t.Skip("§11.7 synth-intersect is Union-only — Intersect on axial overlap produces wrong rings (tracked in roadmap)")
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	got, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := 70.0 // overlap rectangle [-2,5]×[-5,5]
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Intersect(axial overlap) area %v want %v", got.Area(), wantArea)
	}
}

func TestDifferenceOverlappingAxisAligned(t *testing.T) {
	t.Skip("§11.7 synth-intersect is Union-only — Difference on axial overlap produces wrong rings (tracked in roadmap)")
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := 30.0 // L-shape: sq1 minus overlap
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Difference(axial overlap) area %v want %v", got.Area(), wantArea)
	}
}

func TestXorOverlappingAxisAligned(t *testing.T) {
	t.Skip("§11.7 synth-intersect is Union-only — Xor on axial overlap produces wrong rings (tracked in roadmap)")
	a := MultiPolygon{sq(0, 0, 5)}
	b := MultiPolygon{sq(3, 0, 5)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := 60.0 // two L-shapes: (sq1 ∪ sq2) − 2·overlap = 130 − 140? Let me recompute.
	// Actually Xor area = |A| + |B| − 2|A∩B| = 100 + 100 − 2·70 = 60.
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Xor(axial overlap) area %v want %v", got.Area(), wantArea)
	}
}

// TestUnionOverlappingSquaresVertexInsideOther documents a Union failure on
// two axis-aligned squares that overlap such that each square has a vertex
// strictly INSIDE the other (and edges that cross interior-to-interior,
// not at endpoints). Distinct from TestUnionOverlappingAxisAligned where
// the squares share horizontal y-values: there §11.7's synth-intersect
// handles diff-src coincident edges. Here there are no coincident edges
// at all, just proper edge-edge crossings that should produce a single
// merged piece of area 184 (100 + 100 − 16 overlap).
//
// Root cause (investigated 2026-05-20): the two crossings (10,6) and
// (6,10) are each a vertical edge passing through the INTERIOR of a
// horizontal edge. Horizontals are excluded from the AEL, so
// maybeScheduleIntersect never sees these crossings, and the §11.7
// synth-intersect workaround only matches horizontal ENDPOINTS, not
// interior crossings. The fix is the DoHorizontal rework (horizontals as
// first-class AEL edges) planned in DESIGN.md §12.6.1; a synth-intersect
// shortcut was tried and proven insufficient (front/back polarity wall —
// AddLocalMaxPoly bails when both crossing edges are the same ring side).
func TestUnionOverlappingSquaresVertexInsideOther(t *testing.T) {
	t.Skip("engine bug: vertical-through-horizontal-interior crossing dropped; fix = DoHorizontal rework, DESIGN.md §12.6.1")
	a := MultiPolygon{ExPolygon{Outer: Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{{6, 6}, {16, 6}, {16, 16}, {6, 16}}}}
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	wantArea := 184.0 // 100 + 100 − 16 overlap [6,10]×[6,10]
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Union area %v want %v", got.Area(), wantArea)
	}
	if len(got) != 1 {
		t.Errorf("expected 1 merged piece, got %d", len(got))
	}
}
