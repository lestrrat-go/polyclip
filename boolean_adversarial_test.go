package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// ===== §6.2 hand-built adversarial cases =====

func TestDifferenceAnnulus(t *testing.T) {
	// Outer 10x10 square minus inner 4x4 square produces an annulus —
	// outer ring with a hole. The inner square sits strictly inside the
	// outer with no edge touches; coincident edges are not exercised.
	outer := geom.MultiPolygon{sq(0, 0, 10)}
	inner := geom.MultiPolygon{sq(0, 0, 4)}
	got, err := Difference(outer, inner)
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 piece, got %d: %+v", len(got), got)
	require.Len(t, got[0].Holes, 1, "expected 1 hole, got %d", len(got[0].Holes))
	wantArea := outer.Area() - inner.Area()
	require.InDelta(t, wantArea, got.Area(), 0.01, "Difference area %v want %v", got.Area(), wantArea)
}

func TestIntersectAreaInvariantOverlappingDiamonds(t *testing.T) {
	// Area(Union) + Area(Intersect) == Area(A) + Area(B).
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	b := geom.MultiPolygon{diamond(5, 0, 10)}
	u, err := Union(a, b)
	require.NoError(t, err)
	i, err := Intersect(a, b)
	require.NoError(t, err)
	lhs := u.Area() + i.Area()
	rhs := a.Area() + b.Area()
	require.InDelta(t, rhs, lhs, 0.5, "Area(Union)=%v + Area(Intersect)=%v = %v; Area(A)+Area(B)=%v", u.Area(), i.Area(), lhs, rhs)
}

func TestDifferenceOverlappingDiamonds(t *testing.T) {
	// A ∖ B for two overlapping diamonds. Result area = Area(A) − Area(A∩B).
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	b := geom.MultiPolygon{diamond(5, 0, 10)}
	got, err := Difference(a, b)
	require.NoError(t, err)
	require.NotEmpty(t, got, "expected non-empty difference")
	inter, err := Intersect(a, b)
	require.NoError(t, err)
	want := a.Area() - inter.Area()
	require.InDelta(t, want, got.Area(), 0.5, "Difference area %v want %v (=Area(A)−Area(A∩B))", got.Area(), want)
}

func TestXorOverlappingDiamonds(t *testing.T) {
	// Xor(A,B) = symmetric difference. Area = Area(A) + Area(B) − 2·Area(A∩B).
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	b := geom.MultiPolygon{diamond(5, 0, 10)}
	got, err := Xor(a, b)
	require.NoError(t, err)
	require.NotEmpty(t, got, "expected non-empty xor")
	inter, err := Intersect(a, b)
	require.NoError(t, err)
	want := a.Area() + b.Area() - 2*inter.Area()
	require.InDelta(t, want, got.Area(), 0.5, "Xor area %v want %v", got.Area(), want)
}

func TestUnionTouchingAtVertex(t *testing.T) {
	// Two diamonds touching at a single vertex (corner-to-corner). With
	// the source-based disambiguation in BuildLocalMinima, the two rings
	// are traced independently; the merged result is two ExPolygons (the
	// touch doesn't fuse the rings into one).
	a := geom.MultiPolygon{diamond(0, 0, 5)}
	b := geom.MultiPolygon{diamond(10, 0, 5)} // touches a at (5,0)
	got, err := Union(a, b)
	require.NoError(t, err)
	// Total area must equal sum of the two (touch is measure-zero overlap).
	wantArea := a.Area() + b.Area()
	require.InDelta(t, wantArea, got.Area(), 0.5, "Union area %v want %v; got=%+v", got.Area(), wantArea, got)
}

// ===== §6.2 property invariants =====

func TestUnionIdempotent(t *testing.T) {
	// Union(A, A) should equal A (modulo orientation/start-vertex).
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	got, err := Union(a, a)
	require.NoError(t, err)
	require.InDelta(t, a.Area(), got.Area(), 0.5, "Union(A,A) area %v want %v", got.Area(), a.Area())
}

func TestIntersectIdempotent(t *testing.T) {
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	got, err := Intersect(a, a)
	require.NoError(t, err)
	require.InDelta(t, a.Area(), got.Area(), 0.5, "Intersect(A,A) area %v want %v", got.Area(), a.Area())
}

func TestDifferenceSelf(t *testing.T) {
	// Difference(A, A) should be empty.
	a := geom.MultiPolygon{diamond(0, 0, 10)}
	got, err := Difference(a, a)
	require.NoError(t, err)
	require.InDelta(t, 0.0, got.Area(), 0.5, "Diff(A,A) area %v want ≈0", got.Area())
}

// ===== Non-Union adversarial coverage on AXIAL inputs =====

func TestIntersectTouchingBoundaryAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(10, 0, 5)}
	got, err := Intersect(a, b)
	require.NoError(t, err)
	// Touching only at boundary edge — intersection has zero area.
	require.InDelta(t, 0.0, got.Area(), 0.01, "Intersect(touching) area %v want ≈0", got.Area())
}

func TestDifferenceTouchingBoundaryAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(10, 0, 5)}
	got, err := Difference(a, b)
	require.NoError(t, err)
	// Touching boundary doesn't subtract anything from A's area.
	require.InDelta(t, a.Area(), got.Area(), 0.01, "Difference(touching) area %v want %v", got.Area(), a.Area())
}

func TestXorTouchingBoundaryAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(10, 0, 5)}
	got, err := Xor(a, b)
	require.NoError(t, err)
	wantArea := a.Area() + b.Area()
	require.InDelta(t, wantArea, got.Area(), 0.01, "Xor(touching) area %v want %v", got.Area(), wantArea)
}

func TestIntersectNestedAxialSquares(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 10)}
	b := geom.MultiPolygon{sq(0, 0, 3)}
	got, err := Intersect(a, b)
	require.NoError(t, err)
	// Inner square fully contained → intersection equals the inner square.
	require.InDelta(t, b.Area(), got.Area(), 0.01, "Intersect(nested) area %v want %v", got.Area(), b.Area())
}

func TestDifferenceNestedAxialSquares(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 10)}
	b := geom.MultiPolygon{sq(0, 0, 3)}
	got, err := Difference(a, b)
	require.NoError(t, err)
	wantArea := a.Area() - b.Area()
	require.InDelta(t, wantArea, got.Area(), 0.01, "Difference(nested) area %v want %v", got.Area(), wantArea)
}

func TestXorNestedAxialSquares(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 10)}
	b := geom.MultiPolygon{sq(0, 0, 3)}
	got, err := Xor(a, b)
	require.NoError(t, err)
	wantArea := a.Area() - b.Area() // outer minus inner; equivalent to Difference here
	require.InDelta(t, wantArea, got.Area(), 0.01, "Xor(nested) area %v want %v", got.Area(), wantArea)
}

func TestIntersectTouchingAtVertex(t *testing.T) {
	a := geom.MultiPolygon{diamond(0, 0, 5)}
	b := geom.MultiPolygon{diamond(10, 0, 5)}
	got, err := Intersect(a, b)
	require.NoError(t, err)
	require.InDelta(t, 0.0, got.Area(), 0.01, "Intersect(touching vertex) area %v want ≈0", got.Area())
}

func TestDifferenceTouchingAtVertex(t *testing.T) {
	a := geom.MultiPolygon{diamond(0, 0, 5)}
	b := geom.MultiPolygon{diamond(10, 0, 5)}
	got, err := Difference(a, b)
	require.NoError(t, err)
	require.InDelta(t, a.Area(), got.Area(), 0.01, "Difference(touching vertex) area %v want %v", got.Area(), a.Area())
}

// ===== Axial overlapping for non-Union =====
//
// Intersect/Difference/Xor on axial OVERLAPPING (not nested / not touching /
// not disjoint) inputs. These exercise coincident different-source horizontals
// at the overlap; handled by the first-class-horizontal bound model and the
// coincident-horizontal dispatch (DESIGN.md §12.6.1, §12.11).

func TestIntersectOverlappingAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(3, 0, 5)}
	got, err := Intersect(a, b)
	require.NoError(t, err)
	wantArea := 70.0 // overlap rectangle [-2,5]×[-5,5]
	require.InDelta(t, wantArea, got.Area(), 0.5, "Intersect(axial overlap) area %v want %v", got.Area(), wantArea)
}

func TestDifferenceOverlappingAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(3, 0, 5)}
	got, err := Difference(a, b)
	require.NoError(t, err)
	wantArea := 30.0 // L-shape: sq1 minus overlap
	require.InDelta(t, wantArea, got.Area(), 0.5, "Difference(axial overlap) area %v want %v", got.Area(), wantArea)
}

func TestXorOverlappingAxisAligned(t *testing.T) {
	a := geom.MultiPolygon{sq(0, 0, 5)}
	b := geom.MultiPolygon{sq(3, 0, 5)}
	got, err := Xor(a, b)
	require.NoError(t, err)
	wantArea := 60.0 // two L-shapes: (sq1 ∪ sq2) − 2·overlap = 130 − 140? Let me recompute.
	// Actually Xor area = |A| + |B| − 2|A∩B| = 100 + 100 − 2·70 = 60.
	require.InDelta(t, wantArea, got.Area(), 0.5, "Xor(axial overlap) area %v want %v", got.Area(), wantArea)
}

// TestUnionOverlappingSquaresVertexInsideOther covers two axis-aligned squares
// that overlap such that each square has a vertex strictly INSIDE the other.
// The two crossings (10,6) and (6,10) are each a vertical edge passing through
// the INTERIOR of a horizontal edge — exactly the case the DoHorizontal rework
// (DESIGN.md §12.6.1) targets: horizontals are first-class AEL edges, so the
// crossing flows through the normal IntersectEdges path. Expected single
// merged piece of area 184 (100 + 100 − 16 overlap).
func TestUnionOverlappingSquaresVertexInsideOther(t *testing.T) {
	a := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	b := geom.New().Point(6, 6).Point(16, 6).Point(16, 16).Point(6, 16).MustBuild()
	got, err := Union(a, b)
	require.NoError(t, err)
	wantArea := 184.0 // 100 + 100 − 16 overlap [6,10]×[6,10]
	require.InDelta(t, wantArea, got.Area(), 0.5, "Union area %v want %v", got.Area(), wantArea)
	require.Len(t, got, 1, "expected 1 merged piece, got %d", len(got))
}

// TestUnionCoincidentHorizConfluence is a multi-edge-confluence regression
// captured from FuzzUnion after the §12.6.1 DoHorizontal rework. Subject `a`
// and clip `b` share a coincident bottom horizontal at y=-5 (diff-source, same
// direction), AND `a`'s local-maximum apex (-2,5) coincides exactly with `b`'s
// top-left boundary vertex (-2,5) — four bounds reach a local max here, forming
// two same-coordinate maxima pairs (a's at (-2,5), b's at (8,5)) interleaved in
// the AEL as a-L,b-L,a-R,b-R.
//
// Before the fix, closeBound only paired immediate AEL neighbours, so the
// interleaved pairs were mis-handled: `b`'s trailing top horizontal stayed cold
// (the hot/contributing status was never transferred across the confluence) and
// the engine dropped `b`, returning ~65. The fix (DESIGN.md §12.6.1 follow-up)
// makes maximaPartner scan the whole AEL and resolveBetweenMaxima cross the
// between-edges via IntersectEdges, mirroring Clipper2's DoMaxima
// (engine.cpp:2729). Expected area 130: pentagon (-5,-5),(8,-5),(8,5),(-2,5),(-5,5).
func TestUnionCoincidentHorizConfluence(t *testing.T) {
	a := geom.New().Point(-5, -5).Point(5, -5).Point(-2, 5).Point(-5, 5).MustBuild()
	b := geom.New().Point(-2, -5).Point(8, -5).Point(8, 5).Point(-2, 5).MustBuild()
	got, err := Union(a, b)
	require.NoError(t, err)
	wantArea := 130.0
	require.InDelta(t, wantArea, got.Area(), 0.5, "Union area %v want %v", got.Area(), wantArea)
}

// TestUnionSlantCoincidentBottom is a regression captured from FuzzUnion. The
// subject `a` is a slanted quad whose bottom edge (-5,-5)->(16,-5) is partly
// COINCIDENT with the clip square `b`'s bottom edge (15,-5)->(25,-5) over
// x∈[15,16]. Preprocess splits that overlap into a shared segment carried by
// both `a` and `b`. `a`'s slant (16,-5)->(5,5) then crosses `b`'s left edge
// x=15 at (15,-45/11≈-4.09); above that crossing each shape exits the other.
//
// Before the fix the crossing was never scheduled: while the bottom horizontals
// were walked, `b`'s coincident horizontal sat transiently between `b`'s left
// edge and `a`'s slant at the moment their neighbours were checked, then
// advanced away leaving them adjacent with no fresh intersection check. The
// engine produced a single triangle of area 150 — losing the entire top
// boundary. The fix re-scans adjacent AEL pairs after the horizontal pass.
//
// Expected area 254.5454…: |a|+|b| − overlap = 155 + 100 − 5/11.
func TestUnionSlantCoincidentBottom(t *testing.T) {
	a := geom.New().Point(-5, -5).Point(16, -5).Point(5, 5).Point(-5, 5).MustBuild()
	b := geom.New().Point(15, -5).Point(25, -5).Point(25, 5).Point(15, 5).MustBuild()
	got, err := Union(a, b)
	require.NoError(t, err)
	wantArea := 255.0 - 5.0/11.0
	require.InDelta(t, wantArea, got.Area(), 0.01, "Union area %v want %v", got.Area(), wantArea)
	require.Len(t, got, 1, "expected 1 merged piece, got %d", len(got))
}
