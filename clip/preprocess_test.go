package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
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
	if len(out) != len(segs) {
		t.Fatalf("len: got %d want %d", len(out), len(segs))
	}
}

func TestSplitOverlapsDropsDegenerate(t *testing.T) {
	p := fixed.Point{X: 1, Y: 1}
	segs := []Segment{
		{Bot: p, Top: p, Src: Subject}, // degenerate
		segSrc(0, 0, 10, 0, Subject),
	}
	out := SplitOverlaps(segs)
	if len(out) != 1 {
		t.Fatalf("len: %d want 1", len(out))
	}
}

func TestSplitOverlapsPartialOverlap(t *testing.T) {
	// a: [0, 10], b: [5, 15] on the X axis.
	a := segSrc(0, 0, 10, 0, Subject)
	b := segSrc(5, 0, 15, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})

	// Expect four output pieces (in any order):
	//   [0,5] subject, [5,10] subject, [5,10] clip, [10,15] clip
	if len(out) != 4 {
		t.Fatalf("len: %d want 4 — out=%+v", len(out), out)
	}

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
		if have[w] != 1 {
			t.Errorf("missing or duplicated segment: %+v (have count %d)", w, have[w])
		}
	}
}

func TestSplitOverlapsContainment(t *testing.T) {
	// a fully contains b: a=[0,20], b=[5,15] collinear.
	a := segSrc(0, 0, 20, 0, Subject)
	b := segSrc(5, 0, 15, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})

	// Expect five pieces: a splits into [0,5], [5,15], [15,20]; b stays as [5,15].
	if len(out) != 4 {
		t.Fatalf("len: %d want 4 — out=%+v", len(out), out)
	}
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
		if have[w] != 1 {
			t.Errorf("missing or duplicated: %+v", w)
		}
	}
}

func TestSplitOverlapsFullCoincidenceUnchanged(t *testing.T) {
	// Fully coincident segments must remain (the sweep handles the dedup).
	a := segSrc(0, 0, 10, 0, Subject)
	b := segSrc(0, 0, 10, 0, Clip)
	out := SplitOverlaps([]Segment{a, b})
	if len(out) != 2 {
		t.Fatalf("len: %d want 2", len(out))
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
		if !valid[int64(s.Bot.X)] || !valid[int64(s.Top.X)] {
			t.Errorf("segment with unexpected endpoint: %+v", s)
		}
		if s.Degenerate() {
			t.Errorf("degenerate segment in output: %+v", s)
		}
	}
	// No two output segments may overlap (only fully coincide is OK).
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			r := Intersect(out[i], out[j])
			if r.Kind == CollinearOverlap &&
				(out[i].Bot != out[j].Bot || out[i].Top != out[j].Top) {
				t.Errorf("residual overlap between %+v and %+v", out[i], out[j])
			}
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
	if !s.Reversed {
		t.Fatalf("setup: segment should be Reversed=true")
	}
	mid1 := fixed.Point{X: 3, Y: 0}
	mid2 := fixed.Point{X: 7, Y: 0}
	pieces := splitAt(s, mid1, mid2)
	for _, p := range pieces {
		if !p.Reversed {
			t.Errorf("piece lost Reversed flag: %+v", p)
		}
		if p.Src != Subject {
			t.Errorf("piece lost Src: %+v", p)
		}
	}
}
