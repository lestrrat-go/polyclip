package polyclip

import (
	"math"
	"sort"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// mpRect is a unit-test helper building a CCW axis-aligned rectangle MultiPolygon.
func mpRect(x0, y0, x1, y1 float64) geom.MultiPolygon {
	return geom.New().
		Point(x0, y0).Point(x1, y0).Point(x1, y1).Point(x0, y1).
		MustBuild()
}

// TestBuilderMatchesFreeFunctions asserts the accumulator's Execute is
// byte-identical to the named free functions across overlapping, disjoint,
// identical, empty and multipiece inputs — the step-0 behavior-preserving
// contract (DESIGN.md §7.8).
func TestBuilderMatchesFreeFunctions(t *testing.T) {
	overlapA := mpRect(0, 0, 4, 4)
	overlapB := mpRect(2, 2, 6, 6)
	disjointB := mpRect(10, 10, 12, 12)
	multiA := geom.MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	carveB := mpRect(1, -1, 3, 5)

	cases := []struct {
		name string
		a, b geom.MultiPolygon
	}{
		{"overlap", overlapA, overlapB},
		{"disjoint", overlapA, disjointB},
		{"identical", overlapA, overlapA},
		{"emptyClip", overlapA, geom.MultiPolygon{}},
		{"emptySubject", geom.MultiPolygon{}, overlapB},
		{"bothEmpty", geom.MultiPolygon{}, geom.MultiPolygon{}},
		{"multipieceDiff", multiA, carveB},
	}

	ops := []struct {
		op   Operation
		free func(a, b geom.MultiPolygon) (geom.MultiPolygon, error)
	}{
		{OpUnion, Union},
		{OpIntersect, Intersect},
		{OpDifference, Difference},
		{OpXor, Xor},
	}

	for _, tc := range cases {
		for _, o := range ops {
			want, werr := o.free(tc.a, tc.b)
			got, gerr := New().AddSubject(tc.a).AddClip(tc.b).Execute(o.op)
			require.Equal(t, werr == nil, gerr == nil, "%s op=%d: error mismatch free=%v clipper=%v", tc.name, o.op, werr, gerr)
			if werr != nil {
				continue
			}
			require.True(t, mpolyEqual(want, got.Closed), "%s op=%d: Execute=%v want free=%v", tc.name, o.op, got.Closed, want)
			require.Nil(t, got.Open, "%s op=%d: Open should be nil, got %v", tc.name, o.op, got.Open)
		}
	}
}

// TestBuilderAccumulatesAndResets checks that multiple Add* calls aggregate
// their pieces into a single subject/clip set, that Execute is non-destructive
// (repeatable), and that Reset clears the inputs.
func TestBuilderAccumulatesAndResets(t *testing.T) {
	c := New().
		AddSubject(mpRect(0, 0, 2, 2)).
		AddSubject(mpRect(0, 4, 2, 6)).
		AddClip(mpRect(1, -1, 3, 5))

	// Aggregated two subject pieces against one clip == one multipiece subject.
	wantSubj := geom.MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	want, err := Difference(wantSubj, mpRect(1, -1, 3, 5))
	require.NoError(t, err)

	first, err := c.Execute(OpDifference)
	require.NoError(t, err)
	require.True(t, mpolyEqual(want, first.Closed), "accumulated Difference=%v want %v", first.Closed, want)

	// Execute is non-destructive: a second call yields the same result.
	second, err := c.Execute(OpDifference)
	require.NoError(t, err)
	require.True(t, mpolyEqual(first.Closed, second.Closed), "second Execute=%v differs from first %v", second.Closed, first.Closed)

	// Reset clears inputs: Difference of nothing is empty.
	got, err := c.Reset().Execute(OpDifference)
	require.NoError(t, err)
	require.Empty(t, got.Closed, "after Reset, Execute=%v want empty", got.Closed)
}

// TestBuilderUnknownOperation asserts an out-of-range Operation errors rather
// than silently producing wrong output.
func TestBuilderUnknownOperation(t *testing.T) {
	_, err := New().AddSubject(mpRect(0, 0, 1, 1)).Execute(Operation(99))
	require.Error(t, err, "Execute with unknown op: want error, got nil")
}

// exRect builds a CCW axis-aligned rectangle as a single ExPolygon.
func exRect(x0, y0, x1, y1 float64) geom.ExPolygon {
	return geom.New().
		Point(x0, y0).Point(x1, y0).Point(x1, y1).Point(x0, y1).
		MustBuild()[0]
}

// countHoles returns the total number of holes across every piece.
func countHoles(m geom.MultiPolygon) int {
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
	subj := geom.MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	eo, err := New().AddSubject(subj).Fill(FillEvenOdd).Execute(OpUnion)
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
	subj := geom.MultiPolygon{exRect(0, 0, 4, 4), exRect(1, 1, 3, 3)}

	eo, err := New().AddSubject(subj).Fill(FillEvenOdd).Execute(OpUnion)
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
	subj := geom.MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	got, err := New().AddSubject(subj).Fill(FillEvenOdd).Execute(OpDifference)
	require.NoError(t, err)
	require.InDelta(t, 6, got.Closed.Area(), 1e-9, "even-odd difference area = %v, want 6", got.Closed.Area())
	require.Equal(t, 1, countHoles(got.Closed), "even-odd difference holes, want 1")
}

// TestEvenOddWellFormedEqualsNonZero: for simple, non-self-overlapping inputs
// the even-odd and non-zero rules agree. Across all four ops on two distinct
// overlapping squares the result areas match under either rule.
func TestEvenOddWellFormedEqualsNonZero(t *testing.T) {
	a := geom.MultiPolygon{exRect(0, 0, 2, 2)}
	b := geom.MultiPolygon{exRect(1, 1, 3, 3)}

	for _, op := range []Operation{OpUnion, OpIntersect, OpDifference, OpXor} {
		eo, err := New().AddSubject(a).AddClip(b).Fill(FillEvenOdd).Execute(op)
		require.NoError(t, err, "op=%d even-odd", op)
		nz, err := New().AddSubject(a).AddClip(b).Execute(op)
		require.NoError(t, err, "op=%d non-zero", op)
		require.InDelta(t, nz.Closed.Area(), eo.Closed.Area(), 1e-9, "op=%d: even-odd area %v != non-zero area %v", op, eo.Closed.Area(), nz.Closed.Area())
	}
}

// TestBuilderFillDefaultIsNonZero: a Builder with no Fill call matches both an
// explicit FillNonZero and the named free function, byte-for-byte.
func TestBuilderFillDefaultIsNonZero(t *testing.T) {
	a := geom.MultiPolygon{exRect(0, 0, 4, 4)}
	b := geom.MultiPolygon{exRect(2, 2, 6, 6)}

	def, err := New().AddSubject(a).AddClip(b).Execute(OpUnion)
	require.NoError(t, err)
	explicit, err := New().AddSubject(a).AddClip(b).Fill(FillNonZero).Execute(OpUnion)
	require.NoError(t, err)
	free, err := Union(a, b)
	require.NoError(t, err)
	require.True(t, mpolyEqual(def.Closed, free), "default-fill Builder != free Union")
	require.True(t, mpolyEqual(explicit.Closed, free), "explicit FillNonZero Builder != free Union")
}

// TestResetClearsFill: Reset restores the default fill rule.
func TestResetClearsFill(t *testing.T) {
	subj := geom.MultiPolygon{exRect(0, 0, 2, 2), exRect(1, 1, 3, 3)}

	b := New().AddSubject(subj).Fill(FillEvenOdd)
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

// treeRect returns an axis-aligned rectangle ring (CCW).
func treeRect(x0, y0, x1, y1 float64) geom.Polygon {
	return geom.New().
		Point(x0, y0).Point(x1, y0).Point(x1, y1).Point(x0, y1).
		MustPolygon()
}

// flattenPolyTree reproduces assembleResult's flattening rule: each filled
// node becomes an ExPolygon whose holes are its direct children, and every
// island nested inside a hole becomes its own top-level ExPolygon.
func flattenPolyTree(t *PolyTree) geom.MultiPolygon {
	var out geom.MultiPolygon
	var walk func(n *PolyTreeNode)
	walk = func(n *PolyTreeNode) {
		if n.IsHole {
			for _, c := range n.Children { // islands inside the hole
				walk(c)
			}
			return
		}
		ex := geom.ExPolygon{Outer: n.Polygon}
		for _, h := range n.Children { // a filled node's children are its holes
			ex.Holes = append(ex.Holes, h.Polygon)
			for _, gc := range h.Children {
				walk(gc)
			}
		}
		out = append(out, ex)
	}
	for _, c := range t.Children {
		walk(c)
	}
	return out
}

// mpolySignature is an order-independent fingerprint of a MultiPolygon: the
// sorted areas of every ring (outers positive, holes negative). Two
// MultiPolygons with the same pieces (in any order, holes in any order) share
// a signature.
func mpolySignature(m geom.MultiPolygon) []float64 {
	var sig []float64
	for _, ex := range m {
		sig = append(sig, math.Abs(ex.Outer.SignedArea()))
		for _, h := range ex.Holes {
			sig = append(sig, -math.Abs(h.SignedArea()))
		}
	}
	sort.Float64s(sig)
	return sig
}

func sameShape(t *testing.T, want, got geom.MultiPolygon) {
	t.Helper()
	require.Len(t, got, len(want), "piece count: want %d got %d", len(want), len(got))
	ws, gs := mpolySignature(want), mpolySignature(got)
	require.Len(t, gs, len(ws), "ring count: want %d got %d", len(ws), len(gs))
	for i := range ws {
		require.InDelta(t, ws[i], gs[i], 1e-9, "ring areas differ at %d: want %v got %v", i, ws, gs)
	}
}

func TestExecuteTreeFlattensToClosed(t *testing.T) {
	tests := []struct {
		name string
		subj geom.MultiPolygon
		clip geom.MultiPolygon
		op   Operation
	}{
		{
			name: "simple square union empty",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			op:   OpUnion,
		},
		{
			name: "annulus via difference",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			clip: geom.MultiPolygon{{Outer: treeRect(2, 2, 8, 8)}},
			op:   OpDifference,
		},
		{
			name: "two disjoint squares union",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 4, 4)}},
			clip: geom.MultiPolygon{{Outer: treeRect(10, 10, 14, 14)}},
			op:   OpUnion,
		},
		{
			name: "overlap union (one piece)",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: geom.MultiPolygon{{Outer: treeRect(4, 4, 10, 10)}},
			op:   OpUnion,
		},
		{
			name: "xor of overlapping squares",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: geom.MultiPolygon{{Outer: treeRect(3, 3, 9, 9)}},
			op:   OpXor,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := New().AddSubject(tt.subj).AddClip(tt.clip).Execute(tt.op)
			require.NoError(t, err, "Execute")
			tree, err := New().AddSubject(tt.subj).AddClip(tt.clip).ExecuteTree(tt.op)
			require.NoError(t, err, "ExecuteTree")
			sameShape(t, res.Closed, flattenPolyTree(tree))
		})
	}
}

// TestExecuteTreeIslandInHole builds a donut (square with a square hole) plus a
// filled island sitting inside that hole, then verifies the tree nests the
// island under the hole (depth 2) while the flat MultiPolygon keeps it as a
// separate top-level piece.
func TestExecuteTreeIslandInHole(t *testing.T) {
	annulus, err := Difference(
		geom.MultiPolygon{{Outer: treeRect(0, 0, 12, 12)}},
		geom.MultiPolygon{{Outer: treeRect(2, 2, 10, 10)}},
	)
	require.NoError(t, err, "Difference")
	donut, err := Union(annulus, geom.MultiPolygon{{Outer: treeRect(4, 4, 8, 8)}})
	require.NoError(t, err, "Union")
	require.Len(t, donut, 2, "donut pieces: want 2 got %d", len(donut)) // flat form: annulus-with-hole + island

	tree, err := New().AddSubject(donut).ExecuteTree(OpUnion)
	require.NoError(t, err, "ExecuteTree")

	require.Len(t, tree.Children, 1, "top-level regions: want 1 got %d", len(tree.Children))
	root := tree.Children[0]
	require.False(t, root.IsHole, "root: IsHole=%v children=%d, want filled with 1 hole", root.IsHole, len(root.Children))
	require.Len(t, root.Children, 1, "root: IsHole=%v children=%d, want filled with 1 hole", root.IsHole, len(root.Children))
	hole := root.Children[0]
	require.True(t, hole.IsHole, "hole: IsHole=%v children=%d, want hole with 1 island", hole.IsHole, len(hole.Children))
	require.Len(t, hole.Children, 1, "hole: IsHole=%v children=%d, want hole with 1 island", hole.IsHole, len(hole.Children))
	island := hole.Children[0]
	require.False(t, island.IsHole, "island: IsHole=%v children=%d, want filled leaf", island.IsHole, len(island.Children))
	require.Len(t, island.Children, 0, "island: IsHole=%v children=%d, want filled leaf", island.IsHole, len(island.Children))
	a := math.Abs(island.Polygon.SignedArea())
	require.InDelta(t, 16.0, a, 1e-9, "island area: want 16 got %v", a)

	// Orientation: filled CCW (>0), hole CW (<0).
	require.Greater(t, root.Polygon.SignedArea(), 0.0, "filled rings must be CCW: root=%v island=%v",
		root.Polygon.SignedArea(), island.Polygon.SignedArea())
	require.Greater(t, island.Polygon.SignedArea(), 0.0, "filled rings must be CCW: root=%v island=%v",
		root.Polygon.SignedArea(), island.Polygon.SignedArea())
	require.Less(t, hole.Polygon.SignedArea(), 0.0, "hole must be CW: %v", hole.Polygon.SignedArea())

	sameShape(t, donut, flattenPolyTree(tree))
}

func TestExecuteTreeEmpty(t *testing.T) {
	square := geom.MultiPolygon{{Outer: treeRect(0, 0, 2, 2)}}
	pt, err := New().
		AddSubject(square).AddClip(square).
		ExecuteTree(OpDifference) // A∖A = ∅
	require.NoError(t, err, "ExecuteTree")
	require.Len(t, pt.Children, 0, "empty result: want 0 children got %d", len(pt.Children))
}
