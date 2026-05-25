package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
	"github.com/stretchr/testify/require"
)

func segSrc(x1, y1, x2, y2 int64, src Source) Segment {
	return NewSegment(
		fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y1)},
		fixed.Point{X: fixed.Coord(x2), Y: fixed.Coord(y2)},
		src,
	)
}

func TestSplitOverlapsNoOverlap(t *testing.T) {
	segs := []Segment{
		segSrc(0, 0, 10, 0, Subject),
		segSrc(0, 5, 10, 5, Subject),  // parallel, not collinear
		segSrc(20, 0, 30, 0, Subject), // collinear but disjoint
	}
	out := SplitOverlaps(segs)
	require.Len(t, out, len(segs), "len: got %d want %d", len(out), len(segs))
}

func TestSplitOverlapsDropsDegenerate(t *testing.T) {
	p := fixed.Point{X: 1, Y: 1}
	segs := []Segment{
		{Bot: p, Top: p, Src: Subject}, // degenerate
		segSrc(0, 0, 10, 0, Subject),
	}
	out := SplitOverlaps(segs)
	require.Len(t, out, 1, "len: %d want 1", len(out))
}

func TestSplitOverlapsPartialOverlap(t *testing.T) {
	// a: [0, 10], b: [5, 15] on the X axis.
	a := segSrc(0, 0, 10, 0, Subject)
	b := segSrc(5, 0, 15, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})

	// Expect four output pieces (in any order):
	//   [0,5] subject, [5,10] subject, [5,10] clip, [10,15] clip
	require.Len(t, out, 4, "len: %d want 4 — out=%+v", len(out), out)

	have := map[Segment]int{}
	for _, s := range out {
		have[s]++
	}

	want := []Segment{
		{Bot: fixed.Point{X: 0, Y: 0}, Top: fixed.Point{X: 5, Y: 0}, Src: Subject},
		{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 10, Y: 0}, Src: Subject},
		{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 10, Y: 0}, Src: Clip},
		{Bot: fixed.Point{X: 10, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Clip},
	}
	for _, w := range want {
		require.Equal(t, 1, have[w], "missing or duplicated segment: %+v (have count %d)", w, have[w])
	}
}

func TestSplitOverlapsContainment(t *testing.T) {
	// a fully contains b: a=[0,20], b=[5,15] collinear.
	a := segSrc(0, 0, 20, 0, Subject)
	b := segSrc(5, 0, 15, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})

	// Expect five pieces: a splits into [0,5], [5,15], [15,20]; b stays as [5,15].
	require.Len(t, out, 4, "len: %d want 4 — out=%+v", len(out), out)
	have := map[Segment]int{}
	for _, s := range out {
		have[s]++
	}
	want := []Segment{
		{Bot: fixed.Point{X: 0, Y: 0}, Top: fixed.Point{X: 5, Y: 0}, Src: Subject},
		{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Subject},
		{Bot: fixed.Point{X: 15, Y: 0}, Top: fixed.Point{X: 20, Y: 0}, Src: Subject},
		{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Clip},
	}
	for _, w := range want {
		require.Equal(t, 1, have[w], "missing or duplicated: %+v", w)
	}
}

func TestSplitOverlapsFullCoincidenceUnchanged(t *testing.T) {
	// Fully coincident segments must remain (the sweep handles the dedup).
	a := segSrc(0, 0, 10, 0, Subject)
	b := segSrc(0, 0, 10, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})
	require.Len(t, out, 2, "len: %d want 2", len(out))
}

func TestSplitOverlapsThreeWay(t *testing.T) {
	// Three collinear overlapping segments — verify the splitter
	// converges. Segments at [0,8], [4,12], [6,10] on the X axis.
	a := segSrc(0, 0, 8, 0, Subject)
	b := segSrc(4, 0, 12, 0, Clip)
	c := segSrc(6, 0, 10, 0, Subject)
	out := SplitOverlaps([]Segment{a, b, c})

	// All output endpoints should be drawn from {0, 4, 6, 8, 10, 12}.
	valid := map[int64]bool{0: true, 4: true, 6: true, 8: true, 10: true, 12: true}
	for _, s := range out {
		require.True(t, valid[int64(s.Bot.X)] && valid[int64(s.Top.X)], "segment with unexpected endpoint: %+v", s)
		require.False(t, s.Degenerate(), "degenerate segment in output: %+v", s)
	}
	// No two output segments may overlap (only fully coincide is OK).
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			r := Intersect(out[i], out[j])
			require.False(t, r.Kind == CollinearOverlap &&
				(out[i].Bot != out[j].Bot || out[i].Top != out[j].Top),
				"residual overlap between %+v and %+v", out[i], out[j])
		}
	}
}

func TestSplitAtPreservesDirection(t *testing.T) {
	// Build a Reversed=true segment manually and split it.
	s := NewSegment(
		fixed.Point{X: 10, Y: 0}, // a
		fixed.Point{X: 0, Y: 0},  // b — produces Reversed=true
		Subject,
	)
	require.True(t, s.Reversed, "setup: segment should be Reversed=true")
	mid1 := fixed.Point{X: 3, Y: 0}
	mid2 := fixed.Point{X: 7, Y: 0}
	pieces := splitAt(s, mid1, mid2)
	for _, p := range pieces {
		require.True(t, p.Reversed, "piece lost Reversed flag: %+v", p)
		require.Equal(t, Subject, p.Src, "piece lost Src: %+v", p)
	}
}

func TestDedupCoincidentEdgesSameSrcSameDir(t *testing.T) {
	// Two identical Subject segments — same Bot/Top/Src/Reversed.
	// DedupCoincidentEdges should keep one, drop the duplicate.
	a := segSrc(0, 0, 10, 10, Subject)
	segs := []Segment{a, a, segSrc(5, 5, 15, 15, Clip)}
	out := DedupCoincidentEdges(segs)
	require.Len(t, out, 2, "len: got %d want 2 (1 dedup + 1 untouched); out=%+v", len(out), out)
}

func TestDedupCoincidentEdgesSameSrcOppositeDir(t *testing.T) {
	// Two Subject segments with opposite input directions (one Reversed,
	// one not) — same canonical Bot/Top. Cancel — drop both.
	fwd := NewSegment(fixed.Point{X: 0, Y: 0}, fixed.Point{X: 10, Y: 10}, Subject)
	rev := NewSegment(fixed.Point{X: 10, Y: 10}, fixed.Point{X: 0, Y: 0}, Subject)
	require.NotEqual(t, rev.Reversed, fwd.Reversed, "test setup: expected opposite Reversed flags; fwd=%+v rev=%+v", fwd, rev)
	segs := []Segment{fwd, rev, segSrc(5, 5, 15, 15, Clip)}
	out := DedupCoincidentEdges(segs)
	require.Len(t, out, 1, "len: got %d want 1 (both cancelled, 1 untouched); out=%+v", len(out), out)
}

func TestDedupCoincidentEdgesDifferentSrcUnchanged(t *testing.T) {
	// Different-source coincident pair — NOT dropped by Dedup (needs the
	// full §11.7 topological merge which isn't implemented yet).
	subj := segSrc(0, 0, 10, 10, Subject)
	clip := NewSegment(fixed.Point{X: 0, Y: 0}, fixed.Point{X: 10, Y: 10}, Clip)
	segs := []Segment{subj, clip}
	out := DedupCoincidentEdges(segs)
	require.Len(t, out, 2, "len: got %d want 2 (diff-src preserved); out=%+v", len(out), out)
}

func TestSplitTJunctionsSplitsInteriorVertex(t *testing.T) {
	// Clip segment's lower endpoint (5,5) lies in the interior of the
	// Subject segment (0,0)->(10,10). Subject must be split there.
	subj := segSrc(0, 0, 10, 10, Subject)
	clp := segSrc(5, 5, 15, 5, Clip)
	out := SplitTJunctions([]Segment{subj, clp})
	require.Len(t, out, 3, "len: got %d want 3 (subj split in two + clip); out=%+v", len(out), out)
	mid := fixed.Point{X: 5, Y: 5}
	var touchingMid int
	for _, s := range out {
		if s.Bot == mid || s.Top == mid {
			touchingMid++
		}
	}
	// The split point is now a shared endpoint of both subject halves and
	// the clip segment.
	require.Equal(t, 3, touchingMid, "segments touching split point (5,5): got %d want 3; out=%+v", touchingMid, out)
}

func TestSplitTJunctionsPreservesSourceAndDirection(t *testing.T) {
	// A reversed subject edge split at an interior vertex keeps Src and the
	// Reversed flag on both halves.
	subj := NewSegment(fixed.Point{X: 10, Y: 10}, fixed.Point{X: 0, Y: 0}, Subject) // reversed
	require.True(t, subj.Reversed, "test setup: expected reversed subject edge")
	clp := segSrc(5, 5, 15, 5, Clip)
	out := SplitTJunctions([]Segment{subj, clp})
	for _, s := range out {
		require.False(t, s.Src == Subject && !s.Reversed, "subject half lost Reversed flag: %+v", s)
	}
}

func TestSplitTJunctionsSharedCornerUnchanged(t *testing.T) {
	// Two segments meeting at a shared endpoint (a corner, not a T-junction)
	// need no split.
	a := segSrc(0, 0, 10, 10, Subject)
	b := segSrc(10, 10, 20, 0, Clip)
	out := SplitTJunctions([]Segment{a, b})
	require.Len(t, out, 2, "len: got %d want 2 (shared corner, no split); out=%+v", len(out), out)
}

func TestSplitTJunctionsNoTouchUnchanged(t *testing.T) {
	a := segSrc(0, 0, 10, 10, Subject)
	b := segSrc(0, 10, 10, 20, Clip) // parallel, disjoint
	out := SplitTJunctions([]Segment{a, b})
	require.Len(t, out, 2, "len: got %d want 2 (disjoint, no split); out=%+v", len(out), out)
}
