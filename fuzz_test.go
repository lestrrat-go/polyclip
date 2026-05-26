package polyclip

import (
	"math"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
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

func makeQuad(x1, y1, x2, y2, x3, y3, x4, y4 int16) geom.MultiPolygon {
	return geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(float64(x1), float64(y1)).
		Point(float64(x2), float64(y2)).
		Point(float64(x3), float64(y3)).
		Point(float64(x4), float64(y4)).
		MustPolygon()}}
}

func nonDegenerate(m geom.MultiPolygon) bool {
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
		if _, _, ok := geom.RingSelfIntersection(ex.Outer); ok {
			return false
		}
	}
	return true
}

func skipFuzzInputs(t *testing.T, a, b geom.MultiPolygon) {
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

// makeHexagon builds a single-ring hexagon. Unlike makeQuad it does NOT
// require the ring to be simple: the points are used verbatim, so the fuzzer
// freely produces self-intersecting (edges cross) and self-touching (two
// vertices snap to one point) rings — the inputs that exercise
// BuildLocalMinima's self-touching-ring decomposition. Coordinates are scaled
// down so the fuzzer's mutations land in a dense range where rings interact,
// cross, and pinch frequently.
func makeHexagon(c [12]int16) geom.MultiPolygon {
	b := geom.New()
	for i := 0; i < 12; i += 2 {
		b = b.Point(float64(c[i])/256, float64(c[i+1])/256)
	}
	poly, err := b.Polygon()
	if err != nil {
		return nil
	}
	return geom.MultiPolygon{geom.ExPolygon{Outer: poly}}
}

// FuzzSelfTouchingIdentities stresses the non-simple-input path that the
// mid-bound-horizontal fix repaired. It feeds a possibly self-intersecting /
// self-touching hexagon against a quad and checks the winding-independent set
// identities U=A+B-I, D=A-I, X=U-I, using each operand's engine-defined
// non-zero-winding area (via Simplify). The area-bound invariants used by the
// simple-input targets do NOT hold for self-intersecting rings, but these
// algebraic identities do — they are the same oracle the differential harness
// and TestMidBoundHorizontalRepro use. The fix's failure mode (the op silently
// becoming a no-op) produces gross identity violations, so a relative tolerance
// catches it while tolerating fixed-point snapping noise (DESIGN.md §7.2).
func FuzzSelfTouchingIdentities(f *testing.F) {
	// A simple convex hexagon vs a square (12 hex coords + 8 quad coords).
	f.Add(int16(-1280), int16(-1280), int16(0), int16(-1600), int16(1280), int16(-1280),
		int16(1280), int16(1280), int16(0), int16(1600), int16(-1280), int16(1280),
		int16(-640), int16(-640), int16(640), int16(-640), int16(640), int16(640), int16(-640), int16(640))
	// A self-crossing hexagon vs a square.
	f.Add(int16(-1280), int16(-1280), int16(1280), int16(1280), int16(1280), int16(-1280),
		int16(-1280), int16(1280), int16(0), int16(0), int16(0), int16(-1280),
		int16(-640), int16(-640), int16(640), int16(-640), int16(640), int16(640), int16(-640), int16(640))

	f.Fuzz(func(t *testing.T,
		hx1, hy1, hx2, hy2, hx3, hy3, hx4, hy4, hx5, hy5, hx6, hy6 int16,
		bx1, by1, bx2, by2, bx3, by3, bx4, by4 int16) {
		a := makeHexagon([12]int16{hx1, hy1, hx2, hy2, hx3, hy3, hx4, hy4, hx5, hy5, hx6, hy6})
		b := makeQuad(bx1, by1, bx2, by2, bx3, by3, bx4, by4)
		if a == nil || len(b) == 0 {
			t.Skip("degenerate input")
		}
		bbox := a.BoundingBox().Union(b.BoundingBox())
		if math.Abs(bbox.Max.X-bbox.Min.X) < 1 || math.Abs(bbox.Max.Y-bbox.Min.Y) < 1 {
			t.Skip("degenerate bbox")
		}

		// Helper: run op; require nil or the one documented error; signal skip
		// (via ok=false) on the documented error.
		run := func(m geom.MultiPolygon, err error) (float64, bool) {
			require.True(t, err == nil || err == ErrHorizontalNotSupported, "unexpected error: %v", err)
			if err != nil {
				return 0, false
			}
			return m.Area(), true
		}
		aA, ok1 := run(Simplify(a))
		bA, ok2 := run(Simplify(b))
		uA, ok3 := run(Union(a, b))
		iA, ok4 := run(Intersect(a, b))
		dA, ok5 := run(Difference(a, b))
		xA, ok6 := run(Xor(a, b))
		if !ok1 || !ok2 || !ok3 || !ok4 || !ok5 || !ok6 {
			return
		}

		// Relative tolerance: snapping noise scales with the operand areas; the
		// fix's failure mode (a dropped region) does not, so it stays catchable.
		tol := 1e-3 * (1 + aA + bA + uA)
		require.InDeltaf(t, aA+bA-iA, uA, tol, "Union identity: U=%g want A+B-I=%g (A=%g B=%g I=%g)", uA, aA+bA-iA, aA, bA, iA)
		require.InDeltaf(t, aA-iA, dA, tol, "Difference identity: D=%g want A-I=%g", dA, aA-iA)
		require.InDeltaf(t, uA-iA, xA, tol, "Xor identity: X=%g want U-I=%g", xA, uA-iA)
	})
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
