package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func TestSweep(t *testing.T) {
	const skip = -1 // sentinel: do not assert this count

	cases := []struct {
		name string
		segs []Segment
		// Expected event counts; skip (-1) means "do not check".
		wantLen int // -1 to skip length check
		empty   bool
		wantInt int // EventIntersection count, skip to ignore
		wantBot int // EventBot count, skip to ignore
		wantTop int // EventTop count, skip to ignore
		assert  func(t *testing.T, r *SweepResult)
	}{
		{
			name:    "Empty",
			segs:    nil,
			empty:   true,
			wantLen: skip,
			wantInt: skip,
			wantBot: skip,
			wantTop: skip,
		},
		{
			name:    "SingleSegment",
			segs:    []Segment{segSrc(0, 0, 10, 5, Subject)},
			wantLen: 2,
			wantInt: skip,
			wantBot: skip,
			wantTop: skip,
			assert: func(t *testing.T, r *SweepResult) {
				require.True(t, r.Trace[0].Kind == EventBot && r.Trace[1].Kind == EventTop, "trace kinds: %v %v want Bot Top", r.Trace[0].Kind, r.Trace[1].Kind)
			},
		},
		{
			name: "TwoNonCrossing",
			// Two parallel diagonal segments — no intersection.
			segs: []Segment{
				segSrc(0, 0, 10, 10, Subject),
				segSrc(20, 0, 30, 10, Clip),
			},
			wantLen: skip,
			wantInt: 0,
			wantBot: 2,
			wantTop: 2,
		},
		{
			name: "TwoCrossing",
			// Classic X: two segments crossing at (5, 5).
			segs: []Segment{
				segSrc(0, 0, 10, 10, Subject),
				segSrc(0, 10, 10, 0, Clip),
			},
			wantLen: skip,
			wantInt: 1,
			wantBot: skip,
			wantTop: skip,
			assert: func(t *testing.T, r *SweepResult) {
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
			},
		},
		{
			name: "EventOrdering",
			// Two segments at different Y ranges; verify Y order is respected.
			segs: []Segment{
				segSrc(0, 0, 1, 1, Subject), // Y in [0, 1]
				segSrc(5, 10, 6, 11, Clip),  // Y in [10, 11]
			},
			// Trace should be (Bot at Y=0, Top at Y=1, Bot at Y=10, Top at Y=11).
			wantLen: 4,
			wantInt: skip,
			wantBot: skip,
			wantTop: skip,
			assert: func(t *testing.T, r *SweepResult) {
				wantY := []int64{0, 1, 10, 11}
				for i, want := range wantY {
					require.Equal(t, want, int64(r.Trace[i].P.Y), "Trace[%d].P.Y = %d want %d", i, r.Trace[i].P.Y, want)
				}
			},
		},
		{
			name: "HorizontalRecorded",
			// A horizontal segment generates a single EventHoriz, not Bot+Top.
			segs:    []Segment{segSrc(0, 5, 10, 5, Subject)},
			wantLen: 1,
			wantInt: skip,
			wantBot: skip,
			wantTop: skip,
			assert: func(t *testing.T, r *SweepResult) {
				require.Equal(t, EventHoriz, r.Trace[0].Kind, "kind: %v want EventHoriz", r.Trace[0].Kind)
			},
		},
		{
			name: "DegenerateDropped",
			segs: func() []Segment {
				p := fixed.Point{X: 1, Y: 1}
				return []Segment{{Bot: p, Top: p, Src: Subject}}
			}(),
			empty:   true,
			wantLen: skip,
			wantInt: skip,
			wantBot: skip,
			wantTop: skip,
		},
		{
			name: "ThreeCrossings",
			// Three segments meeting in a small region; just verify the sweep
			// terminates and intersection-event count is sensible.
			segs: []Segment{
				segSrc(0, 0, 10, 10, Subject),
				segSrc(0, 10, 10, 0, Subject),
				segSrc(-5, 5, 15, 5, Clip), // horizontal — will be EventHoriz
			},
			// The two diagonals cross at (5, 5). The horizontal is not added to
			// the AEL in this skeleton, so it does not produce intersection events.
			wantLen: skip,
			wantInt: 1,
			wantBot: skip,
			wantTop: skip,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := Sweep(tc.segs, OpUnion)
			if tc.empty {
				require.Empty(t, r.Trace, "produced trace: %+v", r.Trace)
			}
			if tc.wantLen != skip {
				require.Len(t, r.Trace, tc.wantLen, "trace length: %d want %d", len(r.Trace), tc.wantLen)
			}
			if tc.wantInt != skip {
				require.Equal(t, tc.wantInt, countKind(r.Trace, EventIntersection), "intersection events: %d want %d; trace=%+v", countKind(r.Trace, EventIntersection), tc.wantInt, r.Trace)
			}
			if tc.wantBot != skip {
				require.Equal(t, tc.wantBot, countKind(r.Trace, EventBot), "Bot events: %d want %d", countKind(r.Trace, EventBot), tc.wantBot)
			}
			if tc.wantTop != skip {
				require.Equal(t, tc.wantTop, countKind(r.Trace, EventTop), "Top events: %d want %d", countKind(r.Trace, EventTop), tc.wantTop)
			}
			if tc.assert != nil {
				tc.assert(t, r)
			}
		})
	}
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
