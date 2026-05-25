package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
	"github.com/stretchr/testify/require"
)

// TestInvariantsOverlappingDiamonds runs the sweep on two overlapping
// CCW diamonds and verifies DESIGN.md §11.10 invariants hold on the
// final state.
func TestInvariantsOverlappingDiamonds(t *testing.T) {
	segs := diamondSegs(0, 0, 10, Subject)
	segs = append(segs, diamondSegs(5, 0, 10, Clip)...)
	segs = SplitOverlaps(segs)
	segs = DedupCoincidentEdges(segs)
	sw := Sweep(segs, OpUnion)
	require.NoError(t, sw.Err, "sweep")
	require.NoError(t, CheckInvariants(sw, segs), "invariant violation")
}

// TestInvariantsAxialSquares checks the §11.7 synth-intersect path's
// output still satisfies the (weakened) §11.10 post-conditions.
func TestInvariantsAxialSquares(t *testing.T) {
	segs := squareSegs(0, 0, 5, Subject)
	segs = append(segs, squareSegs(3, 0, 5, Clip)...)
	segs = SplitOverlaps(segs)
	segs = DedupCoincidentEdges(segs)
	sw := Sweep(segs, OpUnion)
	require.NoError(t, sw.Err, "sweep")
	require.NoError(t, CheckInvariants(sw, segs), "invariant violation")
}

func squareSegs(cx, cy, half fixed.Coord, src Source) []Segment {
	pts := []fixed.Point{
		{X: cx - half, Y: cy - half},
		{X: cx + half, Y: cy - half},
		{X: cx + half, Y: cy + half},
		{X: cx - half, Y: cy + half},
	}
	return ringSegs(pts, src)
}

func diamondSegs(cx, cy, r fixed.Coord, src Source) []Segment {
	pts := []fixed.Point{
		{X: cx, Y: cy - r},
		{X: cx + r, Y: cy},
		{X: cx, Y: cy + r},
		{X: cx - r, Y: cy},
	}
	return ringSegs(pts, src)
}

func ringSegs(pts []fixed.Point, src Source) []Segment {
	out := make([]Segment, 0, len(pts))
	for i := range pts {
		j := (i + 1) % len(pts)
		s := NewSegment(pts[i], pts[j], src)
		if !s.Degenerate() {
			out = append(out, s)
		}
	}
	return out
}
