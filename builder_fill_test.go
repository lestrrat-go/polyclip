package polyclip

import (
	"math"
	"testing"
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
	if err != nil {
		t.Fatal(err)
	}
	if got := eo.Closed.Area(); math.Abs(got-6) > 1e-9 {
		t.Errorf("even-odd union area = %v, want 6", got)
	}
	if h := countHoles(eo.Closed); h != 1 {
		t.Errorf("even-odd union holes = %d, want 1 (overlap is a hole)", h)
	}

	// NonZero self-resolution (Simplify) fills the doubly-covered overlap.
	nz, err := Simplify(subj)
	if err != nil {
		t.Fatal(err)
	}
	if got := nz.Area(); math.Abs(got-7) > 1e-9 {
		t.Errorf("non-zero (Simplify) area = %v, want 7", got)
	}
	if h := countHoles(nz); h != 0 {
		t.Errorf("non-zero (Simplify) holes = %d, want 0", h)
	}
}

// TestEvenOddNestedSquaresAnnulus: a larger square with a smaller one fully
// inside, both wound CCW, fed as one subject set. Even-odd makes the
// doubly-covered inner region a hole → an annulus of area 16−4 = 12 with one
// hole. NonZero fills the whole outer (area 16, no hole).
func TestEvenOddNestedSquaresAnnulus(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 4, 4), exRect(1, 1, 3, 3)}

	eo, err := NewBuilder().AddSubject(subj).Fill(FillEvenOdd).Execute(OpUnion)
	if err != nil {
		t.Fatal(err)
	}
	if got := eo.Closed.Area(); math.Abs(got-12) > 1e-9 {
		t.Errorf("even-odd annulus area = %v, want 12", got)
	}
	if h := countHoles(eo.Closed); h != 1 {
		t.Errorf("even-odd annulus holes = %d, want 1", h)
	}

	// NonZero self-resolution (Simplify) fills the nested square solid.
	nz, err := Simplify(subj)
	if err != nil {
		t.Fatal(err)
	}
	if got := nz.Area(); math.Abs(got-16) > 1e-9 {
		t.Errorf("non-zero (Simplify) area = %v, want 16", got)
	}
}

// TestEvenOddDifferenceEmptyClipResolves: even-odd Difference with an empty clip
// must re-resolve the self-overlapping subject (it cannot return it verbatim
// like the NonZero short-circuit). Same overlapping pair → area 6 with a hole.
func TestEvenOddDifferenceEmptyClipResolves(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	got, err := NewBuilder().AddSubject(subj).Fill(FillEvenOdd).Execute(OpDifference)
	if err != nil {
		t.Fatal(err)
	}
	if a := got.Closed.Area(); math.Abs(a-6) > 1e-9 {
		t.Errorf("even-odd difference area = %v, want 6", a)
	}
	if h := countHoles(got.Closed); h != 1 {
		t.Errorf("even-odd difference holes = %d, want 1", h)
	}
}

// TestEvenOddWellFormedEqualsNonZero: for simple, non-self-overlapping inputs
// the even-odd and non-zero rules agree. Across all four ops on two distinct
// overlapping squares the result areas match under either rule.
func TestEvenOddWellFormedEqualsNonZero(t *testing.T) {
	a := MultiPolygon{exRect(0, 0, 2, 2)}
	b := MultiPolygon{exRect(1, 1, 3, 3)}

	for _, op := range []Operation{OpUnion, OpIntersect, OpDifference, OpXor} {
		eo, err := NewBuilder().AddSubject(a).AddClip(b).Fill(FillEvenOdd).Execute(op)
		if err != nil {
			t.Fatalf("op=%d even-odd: %v", op, err)
		}
		nz, err := NewBuilder().AddSubject(a).AddClip(b).Execute(op)
		if err != nil {
			t.Fatalf("op=%d non-zero: %v", op, err)
		}
		if math.Abs(eo.Closed.Area()-nz.Closed.Area()) > 1e-9 {
			t.Errorf("op=%d: even-odd area %v != non-zero area %v", op, eo.Closed.Area(), nz.Closed.Area())
		}
	}
}

// TestBuilderFillDefaultIsNonZero: a Builder with no Fill call matches both an
// explicit FillNonZero and the named free function, byte-for-byte.
func TestBuilderFillDefaultIsNonZero(t *testing.T) {
	a := MultiPolygon{exRect(0, 0, 4, 4)}
	b := MultiPolygon{exRect(2, 2, 6, 6)}

	def, err := NewBuilder().AddSubject(a).AddClip(b).Execute(OpUnion)
	if err != nil {
		t.Fatal(err)
	}
	explicit, err := NewBuilder().AddSubject(a).AddClip(b).Fill(FillNonZero).Execute(OpUnion)
	if err != nil {
		t.Fatal(err)
	}
	free, err := Union(a, b)
	if err != nil {
		t.Fatal(err)
	}
	if !mpolyEqual(def.Closed, free) {
		t.Error("default-fill Builder != free Union")
	}
	if !mpolyEqual(explicit.Closed, free) {
		t.Error("explicit FillNonZero Builder != free Union")
	}
}

// TestResetClearsFill: Reset restores the default fill rule.
func TestResetClearsFill(t *testing.T) {
	subj := MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	b := NewBuilder().AddSubject(subj).Fill(FillEvenOdd)
	eo, err := b.Execute(OpUnion)
	if err != nil {
		t.Fatal(err)
	}
	if got := eo.Closed.Area(); math.Abs(got-6) > 1e-9 {
		t.Errorf("even-odd area = %v, want 6", got)
	}

	// After Reset the fill is FillNonZero again, whose Union with an empty clip
	// short-circuits to the subject verbatim (area 8 = both squares summed),
	// distinct from even-odd's resolved 6 — proving the fill was cleared.
	b.Reset().AddSubject(subj)
	nz, err := b.Execute(OpUnion)
	if err != nil {
		t.Fatal(err)
	}
	if got := nz.Closed.Area(); math.Abs(got-8) > 1e-9 {
		t.Errorf("after Reset area = %v, want 8 (FillNonZero restored)", got)
	}
}
