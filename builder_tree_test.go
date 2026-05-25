package polyclip_test

import (
	"math"
	"sort"
	"testing"

	"github.com/lestrrat-go/polyclip"
)

// treeRect returns an axis-aligned rectangle ring (CCW).
func treeRect(x0, y0, x1, y1 float64) polyclip.Polygon {
	return polyclip.Polygon{
		{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1},
	}
}

// flattenPolyTree reproduces assembleResult's flattening rule: each filled
// node becomes an ExPolygon whose holes are its direct children, and every
// island nested inside a hole becomes its own top-level ExPolygon.
func flattenPolyTree(t *polyclip.PolyTree) polyclip.MultiPolygon {
	var out polyclip.MultiPolygon
	var walk func(n *polyclip.PolyTreeNode)
	walk = func(n *polyclip.PolyTreeNode) {
		if n.IsHole {
			for _, c := range n.Children { // islands inside the hole
				walk(c)
			}
			return
		}
		ex := polyclip.ExPolygon{Outer: n.Polygon}
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
func mpolySignature(m polyclip.MultiPolygon) []float64 {
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

func sameShape(t *testing.T, want, got polyclip.MultiPolygon) {
	t.Helper()
	if len(want) != len(got) {
		t.Fatalf("piece count: want %d got %d", len(want), len(got))
	}
	ws, gs := mpolySignature(want), mpolySignature(got)
	if len(ws) != len(gs) {
		t.Fatalf("ring count: want %d got %d", len(ws), len(gs))
	}
	for i := range ws {
		if math.Abs(ws[i]-gs[i]) > 1e-9 {
			t.Fatalf("ring areas differ at %d: want %v got %v", i, ws, gs)
		}
	}
}

func TestExecuteTreeFlattensToClosed(t *testing.T) {
	tests := []struct {
		name string
		subj polyclip.MultiPolygon
		clip polyclip.MultiPolygon
		op   polyclip.Operation
	}{
		{
			name: "simple square union empty",
			subj: polyclip.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "annulus via difference",
			subj: polyclip.MultiPolygon{{Outer: treeRect(0, 0, 10, 10)}},
			clip: polyclip.MultiPolygon{{Outer: treeRect(2, 2, 8, 8)}},
			op:   polyclip.OpDifference,
		},
		{
			name: "two disjoint squares union",
			subj: polyclip.MultiPolygon{{Outer: treeRect(0, 0, 4, 4)}},
			clip: polyclip.MultiPolygon{{Outer: treeRect(10, 10, 14, 14)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "overlap union (one piece)",
			subj: polyclip.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: polyclip.MultiPolygon{{Outer: treeRect(4, 4, 10, 10)}},
			op:   polyclip.OpUnion,
		},
		{
			name: "xor of overlapping squares",
			subj: polyclip.MultiPolygon{{Outer: treeRect(0, 0, 6, 6)}},
			clip: polyclip.MultiPolygon{{Outer: treeRect(3, 3, 9, 9)}},
			op:   polyclip.OpXor,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := polyclip.NewBuilder().AddSubject(tt.subj).AddClip(tt.clip).Execute(tt.op)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			tree, err := polyclip.NewBuilder().AddSubject(tt.subj).AddClip(tt.clip).ExecuteTree(tt.op)
			if err != nil {
				t.Fatalf("ExecuteTree: %v", err)
			}
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
		polyclip.MultiPolygon{{Outer: treeRect(0, 0, 12, 12)}},
		polyclip.MultiPolygon{{Outer: treeRect(2, 2, 10, 10)}},
	)
	if err != nil {
		t.Fatalf("Difference: %v", err)
	}
	donut, err := polyclip.Union(annulus, polyclip.MultiPolygon{{Outer: treeRect(4, 4, 8, 8)}})
	if err != nil {
		t.Fatalf("Union: %v", err)
	}
	if len(donut) != 2 { // flat form: annulus-with-hole + island
		t.Fatalf("donut pieces: want 2 got %d", len(donut))
	}

	tree, err := polyclip.NewBuilder().AddSubject(donut).ExecuteTree(polyclip.OpUnion)
	if err != nil {
		t.Fatalf("ExecuteTree: %v", err)
	}

	if len(tree.Children) != 1 {
		t.Fatalf("top-level regions: want 1 got %d", len(tree.Children))
	}
	root := tree.Children[0]
	if root.IsHole || len(root.Children) != 1 {
		t.Fatalf("root: IsHole=%v children=%d, want filled with 1 hole", root.IsHole, len(root.Children))
	}
	hole := root.Children[0]
	if !hole.IsHole || len(hole.Children) != 1 {
		t.Fatalf("hole: IsHole=%v children=%d, want hole with 1 island", hole.IsHole, len(hole.Children))
	}
	island := hole.Children[0]
	if island.IsHole || len(island.Children) != 0 {
		t.Fatalf("island: IsHole=%v children=%d, want filled leaf", island.IsHole, len(island.Children))
	}
	if a := math.Abs(island.Polygon.SignedArea()); math.Abs(a-16.0) > 1e-9 {
		t.Fatalf("island area: want 16 got %v", a)
	}

	// Orientation: filled CCW (>0), hole CW (<0).
	if root.Polygon.SignedArea() <= 0 || island.Polygon.SignedArea() <= 0 {
		t.Fatalf("filled rings must be CCW: root=%v island=%v",
			root.Polygon.SignedArea(), island.Polygon.SignedArea())
	}
	if hole.Polygon.SignedArea() >= 0 {
		t.Fatalf("hole must be CW: %v", hole.Polygon.SignedArea())
	}

	sameShape(t, donut, flattenPolyTree(tree))
}

func TestExecuteTreeEmpty(t *testing.T) {
	square := polyclip.MultiPolygon{{Outer: treeRect(0, 0, 2, 2)}}
	pt, err := polyclip.NewBuilder().
		AddSubject(square).AddClip(square).
		ExecuteTree(polyclip.OpDifference) // A∖A = ∅
	if err != nil {
		t.Fatalf("ExecuteTree: %v", err)
	}
	if len(pt.Children) != 0 {
		t.Fatalf("empty result: want 0 children got %d", len(pt.Children))
	}
}
