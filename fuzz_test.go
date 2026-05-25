package polyclip

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// Fuzz infrastructure for the boolean engine. Each FuzzXxx target accepts
// integer coordinates (scaled to float64) for two simple polygons, runs
// the corresponding op, and asserts engine-level invariants:
//
//  - no panic / no infinite loop (default Go fuzz guarantee).
//  - error (if any) is the documented ErrHorizontalNotSupported only.
//  - Area sanity bounds appropriate for the op.
//
// Inputs that produce degenerate polygons (zero or near-zero area) or
// self-intersecting rings are skipped — the engine's contract is simple
// polygons (see [Polygon] doc), so the area invariants below don't hold
// for them, and skipping prevents the fuzzer from chasing them.

const fuzzAreaEps = 1e-6

func makeQuad(x1, y1, x2, y2, x3, y3, x4, y4 int16) MultiPolygon {
	return MultiPolygon{ExPolygon{Outer: Polygon{
		{X: float64(x1), Y: float64(y1)},
		{X: float64(x2), Y: float64(y2)},
		{X: float64(x3), Y: float64(y3)},
		{X: float64(x4), Y: float64(y4)},
	}}}
}

func nonDegenerate(m MultiPolygon) bool {
	if len(m) == 0 {
		return false
	}
	for _, ex := range m {
		if math.Abs(ex.Outer.SignedArea()) < 1e-3 {
			return false
		}
		if !ex.Outer.IsCCW() {
			return false
		}
		// A self-intersecting (bowtie) ring violates the simple-polygon
		// contract: SignedArea/Area no longer equal the true covered area,
		// so the op area bounds below are meaningless. Reuse the engine's
		// own definition of self-intersection.
		if _, _, ok := ringSelfIntersection(ex.Outer); ok {
			return false
		}
	}
	return true
}

func skipFuzzInputs(t *testing.T, a, b MultiPolygon) {
	t.Helper()
	if !nonDegenerate(a) || !nonDegenerate(b) {
		t.Skip("degenerate input")
	}
	// Bound coordinate magnitude to avoid the fixed-point scale degenerating.
	bbox := a.BoundingBox().Union(b.BoundingBox())
	if math.Abs(bbox.Max.X-bbox.Min.X) < 1 || math.Abs(bbox.Max.Y-bbox.Min.Y) < 1 {
		t.Skip("degenerate bbox")
	}
}

// addQuadSeeds registers seed inputs that should pass for ALL boolean
// ops. Cases that exercise §11.7 (diff-src coincident horizontals at
// axial overlap) are seeded only for Union via [addUnionOnlySeeds] —
// Intersect/Difference/Xor don't yet handle that case (see Item 2 in the
// roadmap).
func addQuadSeeds(f *testing.F) {
	// Two disjoint axial squares.
	f.Add(int16(-5), int16(-5), int16(5), int16(-5), int16(5), int16(5), int16(-5), int16(5),
		int16(15), int16(-5), int16(25), int16(-5), int16(25), int16(5), int16(15), int16(5))
	// Two touching-corner squares.
	f.Add(int16(-5), int16(-5), int16(5), int16(-5), int16(5), int16(5), int16(-5), int16(5),
		int16(5), int16(5), int16(15), int16(5), int16(15), int16(15), int16(5), int16(15))
	// Nested: big square + small inside.
	f.Add(int16(-10), int16(-10), int16(10), int16(-10), int16(10), int16(10), int16(-10), int16(10),
		int16(-3), int16(-3), int16(3), int16(-3), int16(3), int16(3), int16(-3), int16(3))
	// Two overlapping diamonds (rotated, no horizontals).
	f.Add(int16(0), int16(-10), int16(10), int16(0), int16(0), int16(10), int16(-10), int16(0),
		int16(5), int16(-10), int16(15), int16(0), int16(5), int16(10), int16(-5), int16(0))
}

// addUnionOnlySeeds adds the §11.7-axial-overlap seed that only Union
// currently handles. Move to [addQuadSeeds] when Item 2 lands.
func addUnionOnlySeeds(f *testing.F) {
	f.Add(int16(-5), int16(-5), int16(5), int16(-5), int16(5), int16(5), int16(-5), int16(5),
		int16(-2), int16(-5), int16(8), int16(-5), int16(8), int16(5), int16(-2), int16(5))
}

func FuzzUnion(f *testing.F) {
	addQuadSeeds(f)
	addUnionOnlySeeds(f)
	f.Fuzz(func(t *testing.T,
		ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4 int16,
		bx1, by1, bx2, by2, bx3, by3, bx4, by4 int16) {
		a := makeQuad(ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4)
		b := makeQuad(bx1, by1, bx2, by2, bx3, by3, bx4, by4)
		skipFuzzInputs(t, a, b)

		got, err := Union(a, b)
		require.True(t, err == nil || err == ErrHorizontalNotSupported, "unexpected error: %v", err)
		if err != nil {
			return
		}
		// Area bounds: max(a,b) ≤ |A∪B| ≤ a+b.
		aArea, bArea, gotArea := a.Area(), b.Area(), got.Area()
		lo := aArea
		if bArea > lo {
			lo = bArea
		}
		hi := aArea + bArea
		require.False(t, gotArea < lo-fuzzAreaEps, "Union area %g < max(a=%g,b=%g)", gotArea, aArea, bArea)
		require.False(t, gotArea > hi+fuzzAreaEps, "Union area %g > a+b=%g", gotArea, hi)
	})
}

func FuzzIntersect(f *testing.F) {
	addQuadSeeds(f)
	f.Fuzz(func(t *testing.T,
		ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4 int16,
		bx1, by1, bx2, by2, bx3, by3, bx4, by4 int16) {
		a := makeQuad(ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4)
		b := makeQuad(bx1, by1, bx2, by2, bx3, by3, bx4, by4)
		skipFuzzInputs(t, a, b)

		got, err := Intersect(a, b)
		require.True(t, err == nil || err == ErrHorizontalNotSupported, "unexpected error: %v", err)
		if err != nil {
			return
		}
		// Area bound: |A∩B| ≤ min(a, b).
		aArea, bArea, gotArea := a.Area(), b.Area(), got.Area()
		hi := aArea
		if bArea < hi {
			hi = bArea
		}
		require.False(t, gotArea > hi+fuzzAreaEps, "Intersect area %g > min(a=%g,b=%g)", gotArea, aArea, bArea)
		require.False(t, gotArea < -fuzzAreaEps, "Intersect area negative: %g", gotArea)
	})
}

func FuzzDifference(f *testing.F) {
	addQuadSeeds(f)
	f.Fuzz(func(t *testing.T,
		ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4 int16,
		bx1, by1, bx2, by2, bx3, by3, bx4, by4 int16) {
		a := makeQuad(ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4)
		b := makeQuad(bx1, by1, bx2, by2, bx3, by3, bx4, by4)
		skipFuzzInputs(t, a, b)

		got, err := Difference(a, b)
		require.True(t, err == nil || err == ErrHorizontalNotSupported, "unexpected error: %v", err)
		if err != nil {
			return
		}
		// Area bound: |A\B| ≤ a.
		require.False(t, got.Area() > a.Area()+fuzzAreaEps, "Difference area %g > a=%g", got.Area(), a.Area())
		require.False(t, got.Area() < -fuzzAreaEps, "Difference area negative: %g", got.Area())
	})
}

func FuzzXor(f *testing.F) {
	addQuadSeeds(f)
	f.Fuzz(func(t *testing.T,
		ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4 int16,
		bx1, by1, bx2, by2, bx3, by3, bx4, by4 int16) {
		a := makeQuad(ax1, ay1, ax2, ay2, ax3, ay3, ax4, ay4)
		b := makeQuad(bx1, by1, bx2, by2, bx3, by3, bx4, by4)
		skipFuzzInputs(t, a, b)

		got, err := Xor(a, b)
		require.True(t, err == nil || err == ErrHorizontalNotSupported, "unexpected error: %v", err)
		if err != nil {
			return
		}
		// Area bound: |A⊕B| ≤ a+b. Lower bound 0.
		require.False(t, got.Area() > a.Area()+b.Area()+fuzzAreaEps, "Xor area %g > a+b=%g", got.Area(), a.Area()+b.Area())
		require.False(t, got.Area() < -fuzzAreaEps, "Xor area negative: %g", got.Area())
	})
}
