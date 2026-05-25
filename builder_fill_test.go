package polyclip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// exRect builds a CCW axis-aligned rectangle as a single ExPolygon.
func exRect(x0, y0, x1, y1 float64) ExPolygon {
	return ExPolygon{Outer: Polygon{
		{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1},
	}}
}

// countHoles returns the total number of holes across every piece.
func countHoles(m MultiPolygon) int {
	n := 0
	for _, ex := range m {
		n += len(ex.Holes)
	}
	return n
}

// TestEvenOddUnionOverlappingSquares: two squares overlapping corner-to-corner,
// fed as a single subject set under even-odd. The doubly-covered overlap reads
// as a hole, so the result is A∪B minus the overlap counted twice: (4+4−1) − 1 =
// 6, with one hole of area 1. The NonZero resolution of the same self-overlap
// (via Simplify) fills the overlap: area 7, no hole.
func TestEvenOddUnionOverlappingSquares(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	eo, err := NewBuilder().AddSubject(subj).Fill(FillEvenOdd).Execute(OpUnion)
	require.NoError(t, err)
	require.InDelta(t, 6, eo.Closed.Area(), 1e-9, "even-odd union area = %v, want 6", eo.Closed.Area())
	require.Equal(t, 1, countHoles(eo.Closed), "even-odd union holes, want 1 (overlap is a hole)")

	// NonZero self-resolution (Simplify) fills the doubly-covered overlap.
	nz, err := Simplify(subj)
	require.NoError(t, err)
	require.InDelta(t, 7, nz.Area(), 1e-9, "non-zero (Simplify) area = %v, want 7", nz.Area())
	require.Equal(t, 0, countHoles(nz), "non-zero (Simplify) holes, want 0")
}

// TestEvenOddNestedSquaresAnnulus: a larger square with a smaller one fully
// inside, both wound CCW, fed as one subject set. Even-odd makes the
// doubly-covered inner region a hole → an annulus of area 16−4 = 12 with one
// hole. NonZero fills the whole outer (area 16, no hole).
func TestEvenOddNestedSquaresAnnulus(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 4, 4), exRect(1, 1, 3, 3)}

	eo, err := NewBuilder().AddSubject(subj).Fill(FillEvenOdd).Execute(OpUnion)
	require.NoError(t, err)
	require.InDelta(t, 12, eo.Closed.Area(), 1e-9, "even-odd annulus area = %v, want 12", eo.Closed.Area())
	require.Equal(t, 1, countHoles(eo.Closed), "even-odd annulus holes, want 1")

	// NonZero self-resolution (Simplify) fills the nested square solid.
	nz, err := Simplify(subj)
	require.NoError(t, err)
	require.InDelta(t, 16, nz.Area(), 1e-9, "non-zero (Simplify) area = %v, want 16", nz.Area())
}

// TestEvenOddDifferenceEmptyClipResolves: even-odd Difference with an empty clip
// must re-resolve the self-overlapping subject (it cannot return it verbatim
// like the NonZero short-circuit). Same overlapping pair → area 6 with a hole.
func TestEvenOddDifferenceEmptyClipResolves(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	got, err := NewBuilder().AddSubject(subj).Fill(FillEvenOdd).Execute(OpDifference)
	require.NoError(t, err)
	require.InDelta(t, 6, got.Closed.Area(), 1e-9, "even-odd difference area = %v, want 6", got.Closed.Area())
	require.Equal(t, 1, countHoles(got.Closed), "even-odd difference holes, want 1")
}

// TestEvenOddWellFormedEqualsNonZero: for simple, non-self-overlapping inputs
// the even-odd and non-zero rules agree. Across all four ops on two distinct
// overlapping squares the result areas match under either rule.
func TestEvenOddWellFormedEqualsNonZero(t *testing.T) {
	a := MultiPolygon{exRect(0, 0, 2, 2)}
	b := MultiPolygon{exRect(1, 1, 3, 3)}

	for _, op := range []Operation{OpUnion, OpIntersect, OpDifference, OpXor} {
		eo, err := NewBuilder().AddSubject(a).AddClip(b).Fill(FillEvenOdd).Execute(op)
		require.NoError(t, err, "op=%d even-odd", op)
		nz, err := NewBuilder().AddSubject(a).AddClip(b).Execute(op)
		require.NoError(t, err, "op=%d non-zero", op)
		require.InDelta(t, nz.Closed.Area(), eo.Closed.Area(), 1e-9, "op=%d: even-odd area %v != non-zero area %v", op, eo.Closed.Area(), nz.Closed.Area())
	}
}

// TestBuilderFillDefaultIsNonZero: a Builder with no Fill call matches both an
// explicit FillNonZero and the named free function, byte-for-byte.
func TestBuilderFillDefaultIsNonZero(t *testing.T) {
	a := MultiPolygon{exRect(0, 0, 4, 4)}
	b := MultiPolygon{exRect(2, 2, 6, 6)}

	def, err := NewBuilder().AddSubject(a).AddClip(b).Execute(OpUnion)
	require.NoError(t, err)
	explicit, err := NewBuilder().AddSubject(a).AddClip(b).Fill(FillNonZero).Execute(OpUnion)
	require.NoError(t, err)
	free, err := Union(a, b)
	require.NoError(t, err)
	require.True(t, mpolyEqual(def.Closed, free), "default-fill Builder != free Union")
	require.True(t, mpolyEqual(explicit.Closed, free), "explicit FillNonZero Builder != free Union")
}

// TestResetClearsFill: Reset restores the default fill rule.
func TestResetClearsFill(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	b := NewBuilder().AddSubject(subj).Fill(FillEvenOdd)
	eo, err := b.Execute(OpUnion)
	require.NoError(t, err)
	require.InDelta(t, 6, eo.Closed.Area(), 1e-9, "even-odd area = %v, want 6", eo.Closed.Area())

	// After Reset the fill is FillNonZero again, whose Union with an empty clip
	// short-circuits to the subject verbatim (area 8 = both squares summed),
	// distinct from even-odd's resolved 6 — proving the fill was cleared.
	b.Reset().AddSubject(subj)
	nz, err := b.Execute(OpUnion)
	require.NoError(t, err)
	require.InDelta(t, 8, nz.Closed.Area(), 1e-9, "after Reset area = %v, want 8 (FillNonZero restored)", nz.Closed.Area())
}
