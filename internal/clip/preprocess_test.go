package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func segSrc(x1, y1, x2, y2 int64, src Source) Segment {
	return NewSegment(
		fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y1)},
		fixed.Point{X: fixed.Coord(x2), Y: fixed.Coord(y2)},
		src,
	)
}

func TestSplitOverlaps(t *testing.T) {
	degen := fixed.Point{X: 1, Y: 1}
	cases := []struct {
		name    string
		segs    []Segment
		wantLen int
		want    []Segment // optional: exact segments expected (each count 1)
	}{
		{
			name: "NoOverlap",
			segs: []Segment{
				segSrc(0, 0, 10, 0, Subject),
				segSrc(0, 5, 10, 5, Subject),  // parallel, not collinear
				segSrc(20, 0, 30, 0, Subject), // collinear but disjoint
			},
			wantLen: 3,
		},
		{
			name: "DropsDegenerate",
			segs: []Segment{
				{Bot: degen, Top: degen, Src: Subject}, // degenerate
				segSrc(0, 0, 10, 0, Subject),
			},
			wantLen: 1,
		},
		{
			name: "PartialOverlap",
			// a: [0, 10], b: [5, 15] on the X axis.
			segs:    []Segment{segSrc(0, 0, 10, 0, Subject), segSrc(5, 0, 15, 0, Clip)},
			wantLen: 4,
			// Expect four output pieces (in any order):
			//   [0,5] subject, [5,10] subject, [5,10] clip, [10,15] clip
			want: []Segment{
				{Bot: fixed.Point{X: 0, Y: 0}, Top: fixed.Point{X: 5, Y: 0}, Src: Subject},
				{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 10, Y: 0}, Src: Subject},
				{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 10, Y: 0}, Src: Clip},
				{Bot: fixed.Point{X: 10, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Clip},
			},
		},
		{
			name: "Containment",
			// a fully contains b: a=[0,20], b=[5,15] collinear.
			segs:    []Segment{segSrc(0, 0, 20, 0, Subject), segSrc(5, 0, 15, 0, Clip)},
			wantLen: 4,
			// Expect: a splits into [0,5], [5,15], [15,20]; b stays as [5,15].
			want: []Segment{
				{Bot: fixed.Point{X: 0, Y: 0}, Top: fixed.Point{X: 5, Y: 0}, Src: Subject},
				{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Subject},
				{Bot: fixed.Point{X: 15, Y: 0}, Top: fixed.Point{X: 20, Y: 0}, Src: Subject},
				{Bot: fixed.Point{X: 5, Y: 0}, Top: fixed.Point{X: 15, Y: 0}, Src: Clip},
			},
		},
		{
			name: "FullCoincidenceUnchanged",
			// Fully coincident segments must remain (the sweep handles the dedup).
			segs:    []Segment{segSrc(0, 0, 10, 0, Subject), segSrc(0, 0, 10, 0, Clip)},
			wantLen: 2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := SplitOverlaps(tc.segs)
			require.Len(t, out, tc.wantLen, "len: %d want %d — out=%+v", len(out), tc.wantLen, out)
			if tc.want != nil {
				have := map[Segment]int{}
				for _, s := range out {
					have[s]++
				}
				for _, w := range tc.want {
					require.Equal(t, 1, have[w], "missing or duplicated segment: %+v (have count %d)", w, have[w])
				}
			}
		})
	}
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

func TestDedupCoincidentEdges(t *testing.T) {
	cases := []struct {
		name    string
		segs    func(t *testing.T) []Segment
		wantLen int
		msg     string
	}{
		{
			name: "SameSrcSameDir",
			// Two identical Subject segments — same Bot/Top/Src/Reversed.
			// DedupCoincidentEdges should keep one, drop the duplicate.
			segs: func(t *testing.T) []Segment {
				a := segSrc(0, 0, 10, 10, Subject)
				return []Segment{a, a, segSrc(5, 5, 15, 15, Clip)}
			},
			wantLen: 2,
			msg:     "len: got %d want 2 (1 dedup + 1 untouched); out=%+v",
		},
		{
			name: "SameSrcOppositeDir",
			// Two Subject segments with opposite input directions (one Reversed,
			// one not) — same canonical Bot/Top. Cancel — drop both.
			segs: func(t *testing.T) []Segment {
				fwd := NewSegment(fixed.Point{X: 0, Y: 0}, fixed.Point{X: 10, Y: 10}, Subject)
				rev := NewSegment(fixed.Point{X: 10, Y: 10}, fixed.Point{X: 0, Y: 0}, Subject)
				require.NotEqual(t, rev.Reversed, fwd.Reversed, "test setup: expected opposite Reversed flags; fwd=%+v rev=%+v", fwd, rev)
				return []Segment{fwd, rev, segSrc(5, 5, 15, 15, Clip)}
			},
			wantLen: 1,
			msg:     "len: got %d want 1 (both cancelled, 1 untouched); out=%+v",
		},
		{
			name: "DifferentSrcUnchanged",
			// Different-source coincident pair — NOT dropped by Dedup (needs the
			// full §11.7 topological merge which isn't implemented yet).
			segs: func(t *testing.T) []Segment {
				subj := segSrc(0, 0, 10, 10, Subject)
				clip := NewSegment(fixed.Point{X: 0, Y: 0}, fixed.Point{X: 10, Y: 10}, Clip)
				return []Segment{subj, clip}
			},
			wantLen: 2,
			msg:     "len: got %d want 2 (diff-src preserved); out=%+v",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := DedupCoincidentEdges(tc.segs(t))
			require.Len(t, out, tc.wantLen, tc.msg, len(out), out)
		})
	}
}

func TestSplitTJunctions(t *testing.T) {
	cases := []struct {
		name    string
		segs    func(t *testing.T) []Segment
		wantLen int
		msg     string
		check   func(t *testing.T, out []Segment) // optional bespoke assertions
	}{
		{
			name: "SplitsInteriorVertex",
			// Clip segment's lower endpoint (5,5) lies in the interior of the
			// Subject segment (0,0)->(10,10). Subject must be split there.
			segs: func(t *testing.T) []Segment {
				return []Segment{segSrc(0, 0, 10, 10, Subject), segSrc(5, 5, 15, 5, Clip)}
			},
			wantLen: 3,
			msg:     "len: got %d want 3 (subj split in two + clip); out=%+v",
			check: func(t *testing.T, out []Segment) {
				mid := fixed.Point{X: 5, Y: 5}
				var touchingMid int
				for _, s := range out {
					if s.Bot == mid || s.Top == mid {
						touchingMid++
					}
				}
				// The split point is now a shared endpoint of both subject
				// halves and the clip segment.
				require.Equal(t, 3, touchingMid, "segments touching split point (5,5): got %d want 3; out=%+v", touchingMid, out)
			},
		},
		{
			name: "PreservesSourceAndDirection",
			// A reversed subject edge split at an interior vertex keeps Src and
			// the Reversed flag on both halves.
			segs: func(t *testing.T) []Segment {
				subj := NewSegment(fixed.Point{X: 10, Y: 10}, fixed.Point{X: 0, Y: 0}, Subject) // reversed
				require.True(t, subj.Reversed, "test setup: expected reversed subject edge")
				return []Segment{subj, segSrc(5, 5, 15, 5, Clip)}
			},
			check: func(t *testing.T, out []Segment) {
				for _, s := range out {
					require.False(t, s.Src == Subject && !s.Reversed, "subject half lost Reversed flag: %+v", s)
				}
			},
		},
		{
			name: "SharedCornerUnchanged",
			// Two segments meeting at a shared endpoint (a corner, not a
			// T-junction) need no split.
			segs: func(t *testing.T) []Segment {
				return []Segment{segSrc(0, 0, 10, 10, Subject), segSrc(10, 10, 20, 0, Clip)}
			},
			wantLen: 2,
			msg:     "len: got %d want 2 (shared corner, no split); out=%+v",
		},
		{
			name: "NoTouchUnchanged",
			segs: func(t *testing.T) []Segment {
				return []Segment{segSrc(0, 0, 10, 10, Subject), segSrc(0, 10, 10, 20, Clip)} // parallel, disjoint
			},
			wantLen: 2,
			msg:     "len: got %d want 2 (disjoint, no split); out=%+v",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := SplitTJunctions(tc.segs(t))
			if tc.msg != "" {
				require.Len(t, out, tc.wantLen, tc.msg, len(out), out)
			}
			if tc.check != nil {
				tc.check(t, out)
			}
		})
	}
}
