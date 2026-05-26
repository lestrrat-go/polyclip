package polyclip_test

import (
	"math"
	"sort"
	"testing"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// treeRect returns an axis-aligned rectangle ring (CCW).
func treeRect(x0, y0, x1, y1 float64) geom.Polygon {
	return geom.New().
		Point(x0, y0).Point(x1, y0).Point(x1, y1).Point(x0, y1).
		MustPolygon()
}

// flattenPolyTree reproduces assembleResult's flattening rule: each filled
// node becomes an ExPolygon whose holes are its direct children, and every
// island nested inside a hole becomes its own top-level ExPolygon.
func flattenPolyTree(t *polyclip.PolyTree) geom.MultiPolygon {
	var out geom.MultiPolygon
	var walk func(n *polyclip.PolyTreeNode)
	walk = func(n *polyclip.PolyTreeNode) {
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
		op   polyclip.Operation
	}{
		{
			name: "simple square union empty",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "annulus via difference",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			clip: geom.MultiPolygon{{Outer: treeRect(2, 2, 8, 8)}},
			op:   polyclip.OpDifference,
		},
		{
			name: "two disjoint squares union",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 4, 4)}},
			clip: geom.MultiPolygon{{Outer: treeRect(10, 10, 14, 14)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "overlap union (one piece)",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: geom.MultiPolygon{{Outer: treeRect(4, 4, 10, 10)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "xor of overlapping squares",
			subj: geom.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: geom.MultiPolygon{{Outer: treeRect(3, 3, 9, 9)}},
			op:   polyclip.OpXor,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := polyclip.New().AddSubject(tt.subj).AddClip(tt.clip).Execute(tt.op)
			require.NoError(t, err, "Execute")
			tree, err := polyclip.New().AddSubject(tt.subj).AddClip(tt.clip).ExecuteTree(tt.op)
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
	annulus, err := polyclip.Difference(
		geom.MultiPolygon{{Outer: treeRect(0, 0, 12, 12)}},
		geom.MultiPolygon{{Outer: treeRect(2, 2, 10, 10)}},
	)
	require.NoError(t, err, "Difference")
	donut, err := polyclip.Union(annulus, geom.MultiPolygon{{Outer: treeRect(4, 4, 8, 8)}})
	require.NoError(t, err, "Union")
	require.Len(t, donut, 2, "donut pieces: want 2 got %d", len(donut)) // flat form: annulus-with-hole + island

	tree, err := polyclip.New().AddSubject(donut).ExecuteTree(polyclip.OpUnion)
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
	pt, err := polyclip.New().
		AddSubject(square).AddClip(square).
		ExecuteTree(polyclip.OpDifference) // A∖A = ∅
	require.NoError(t, err, "ExecuteTree")
	require.Len(t, pt.Children, 0, "empty result: want 0 children got %d", len(pt.Children))
}
