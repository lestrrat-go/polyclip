package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
	"github.com/stretchr/testify/require"
)

func TestSweepEmpty(t *testing.T) {
	r := Sweep(nil, OpUnion)
	require.Empty(t, r.Trace, "empty input produced trace: %+v", r.Trace)
}

func TestSweepSingleSegment(t *testing.T) {
	segs := []Segment{segSrc(0, 0, 10, 5, Subject)}
	r := Sweep(segs, OpUnion)
	require.Len(t, r.Trace, 2, "trace length: %d want 2", len(r.Trace))
	require.True(t, r.Trace[0].Kind == EventBot && r.Trace[1].Kind == EventTop, "trace kinds: %v %v want Bot Top", r.Trace[0].Kind, r.Trace[1].Kind)
}

func TestSweepTwoNonCrossing(t *testing.T) {
	// Two parallel diagonal segments — no intersection.
	segs := []Segment{
		segSrc(0, 0, 10, 10, Subject),
		segSrc(20, 0, 30, 10, Clip),
	}
	r := Sweep(segs, OpUnion)
	require.Equal(t, 0, countKind(r.Trace, EventIntersection), "intersection events: %d want 0", countKind(r.Trace, EventIntersection))
	require.Equal(t, 2, countKind(r.Trace, EventBot), "Bot events: %d want 2", countKind(r.Trace, EventBot))
	require.Equal(t, 2, countKind(r.Trace, EventTop), "Top events: %d want 2", countKind(r.Trace, EventTop))
}

func TestSweepTwoCrossing(t *testing.T) {
	// Classic X: two segments crossing at (5, 5).
	segs := []Segment{
		segSrc(0, 0, 10, 10, Subject),
		segSrc(0, 10, 10, 0, Clip),
	}
	r := Sweep(segs, OpUnion)
	require.Equal(t, 1, countKind(r.Trace, EventIntersection), "intersection events: %d want 1", countKind(r.Trace, EventIntersection))
	// Find the intersection event and verify location.
	var found bool
	for _, te := range r.Trace {
		if te.Kind == EventIntersection {
			if te.P == (fixed.Point{X: 5, Y: 5}) {
				found = true
			}
		}
	}
	require.True(t, found, "no intersection event at (5,5); trace=%+v", r.Trace)
}

func TestSweepEventOrdering(t *testing.T) {
	// Two segments at different Y ranges; verify Y order is respected.
	segs := []Segment{
		segSrc(0, 0, 1, 1, Subject), // Y in [0, 1]
		segSrc(5, 10, 6, 11, Clip),  // Y in [10, 11]
	}
	r := Sweep(segs, OpUnion)
	// Trace should be (Bot at Y=0, Top at Y=1, Bot at Y=10, Top at Y=11).
	require.Len(t, r.Trace, 4, "trace length: %d", len(r.Trace))
	wantY := []int64{0, 1, 10, 11}
	for i, want := range wantY {
		require.Equal(t, want, int64(r.Trace[i].P.Y), "Trace[%d].P.Y = %d want %d", i, r.Trace[i].P.Y, want)
	}
}

func TestSweepHorizontalRecorded(t *testing.T) {
	// A horizontal segment generates a single EventHoriz, not Bot+Top.
	segs := []Segment{segSrc(0, 5, 10, 5, Subject)}
	r := Sweep(segs, OpUnion)
	require.Len(t, r.Trace, 1, "trace length: %d want 1", len(r.Trace))
	require.Equal(t, EventHoriz, r.Trace[0].Kind, "kind: %v want EventHoriz", r.Trace[0].Kind)
}

func TestSweepDegenerateDropped(t *testing.T) {
	p := fixed.Point{X: 1, Y: 1}
	segs := []Segment{{Bot: p, Top: p, Src: Subject}}
	r := Sweep(segs, OpUnion)
	require.Empty(t, r.Trace, "degenerate produced trace: %+v", r.Trace)
}

func TestSweepThreeCrossings(t *testing.T) {
	// Three segments meeting in a small region; just verify the sweep
	// terminates and intersection-event count is sensible.
	segs := []Segment{
		segSrc(0, 0, 10, 10, Subject),
		segSrc(0, 10, 10, 0, Subject),
		segSrc(-5, 5, 15, 5, Clip), // horizontal — will be EventHoriz
	}
	r := Sweep(segs, OpUnion)
	// The two diagonals cross at (5, 5). The horizontal is not added to
	// the AEL in this skeleton, so it does not produce intersection events.
	require.Equal(t, 1, countKind(r.Trace, EventIntersection), "intersection events: %d want 1; trace=%+v", countKind(r.Trace, EventIntersection), r.Trace)
}

func countKind(trace []TraceEvent, k EventKind) int {
	n := 0
	for _, te := range trace {
		if te.Kind == k {
			n++
		}
	}
	return n
}
