package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func TestClassifyHorizontalsAxialRectangle(t *testing.T) {
	// Standard CCW axial rectangle: bottom horizontal is a local minimum,
	// top horizontal is a local maximum.
	segs := axialRect(0, 0, 10, 5, Subject)
	info, err := ClassifyHorizontals(segs)
	require.NoError(t, err)
	require.Len(t, info, 2, "expected 2 horizontals classified, got %d", len(info))

	var bot, top *Segment
	for i := range segs {
		s := &segs[i]
		if !s.Horizontal() {
			continue
		}
		if s.Bot.Y == 0 {
			bot = s
		} else {
			top = s
		}
	}
	require.True(t, bot != nil && top != nil, "missing bottom or top horizontal")

	require.Equal(t, HorizClassMin, info[bot].Class, "bottom horiz class: %v want HorizClassMin", info[bot].Class)
	require.Equal(t, HorizClassMax, info[top].Class, "top horiz class: %v want HorizClassMax", info[top].Class)

	// Adjacency: LeftAdj endpoint should match h.Bot.X; RightAdj should
	// match h.Top.X. For the bottom horiz those endpoints are the verticals'
	// Bot.X; for the top horiz they are the verticals' Top.X.
	checkAdj := func(label string, h *Segment, i *HorizInfo, leftMatch, rightMatch func(*Segment) bool) {
		require.True(t, leftMatch(i.LeftAdj), "%s LeftAdj %v→%v does not match h.Bot=%v", label, i.LeftAdj.Start(), i.LeftAdj.End(), h.Bot)
		require.True(t, rightMatch(i.RightAdj), "%s RightAdj %v→%v does not match h.Top=%v", label, i.RightAdj.Start(), i.RightAdj.End(), h.Top)
	}
	checkAdj("bottom-horiz", bot, info[bot],
		func(s *Segment) bool { return s.Bot == bot.Bot },
		func(s *Segment) bool { return s.Bot == bot.Top },
	)
	checkAdj("top-horiz", top, info[top],
		func(s *Segment) bool { return s.Top == top.Bot },
		func(s *Segment) bool { return s.Top == top.Top },
	)
}

func TestClassifyHorizontalsLoneSegmentIsUnknown(t *testing.T) {
	// A single horizontal not part of any ring: classified as Unknown so
	// the sweep can still emit an EventHoriz trace entry (used by lower-
	// level tests) without erroring out the entire operation.
	segs := []Segment{{
		Bot: fixed.Point{X: 0, Y: 5},
		Top: fixed.Point{X: 10, Y: 5},
		Src: Subject,
	}}
	info, err := ClassifyHorizontals(segs)
	require.NoError(t, err)
	require.Len(t, info, 1, "expected 1 classification, got %d", len(info))
	for _, hi := range info {
		require.Equal(t, HorizClassUnknown, hi.Class, "class: %v want HorizClassUnknown", hi.Class)
	}
}

func TestClassifyHorizontalsMidBoundRejected(t *testing.T) {
	// A staircase step: a non-horizontal edge ascends to a horizontal, the
	// horizontal moves right, then another ascending edge continues up. The
	// horizontal connects an ascending edge to an ascending edge — not a
	// local min/max — and must be rejected.
	src := Subject
	// Input ring (CCW), staircase:
	//   v0(0,0) →up→ v1(0,3) →right→ v2(2,3) →up→ v3(2,6) →left→ v4(0,6)
	//     no, we need a closed ring. Use the simplest closed staircase:
	//   v0(0,0) → v1(2,0) → v2(2,2) → v3(4,2) → v4(4,4) → v5(0,4) → v0.
	pts := []fixed.Point{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2},
		{X: 4, Y: 2}, {X: 4, Y: 4}, {X: 0, Y: 4},
	}
	n := len(pts)
	segs := make([]Segment, 0, n)
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		seg := NewSegment(pts[i], pts[j], src)
		if !seg.Degenerate() {
			segs = append(segs, seg)
		}
	}
	_, err := ClassifyHorizontals(segs)
	require.ErrorIs(t, err, ErrUnsupportedHorizontal, "expected ErrUnsupportedHorizontal, got %v", err)
}
