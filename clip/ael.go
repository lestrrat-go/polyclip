package clip

import (
	"math"
	"sort"

	"github.com/lestrrat-go/polyclip/fixed"
)

// ActiveEdge tracks a single segment as it crosses the active scanline.
type ActiveEdge struct {
	Seg *Segment

	// CurrX is the X coordinate where Seg crosses the current scanline Y.
	// It is updated whenever the scanline advances and at intersection
	// events that reorder neighbours.
	CurrX fixed.Coord

	// WindSelf is the signed winding count of Seg.Src up to and including
	// this edge — i.e. the count of edges of the same source that a
	// left-to-right ray has crossed at this scanline, with sign tracking
	// the input direction. See DESIGN.md §11.3.
	WindSelf int

	// WindOther is the signed winding count of the OTHER source (not
	// Seg.Src) up to but not including this edge. It is not affected by
	// crossing this edge itself.
	WindOther int

	// Contributing records whether this edge participates in the current
	// boolean operation's output, as determined by the classification
	// rule in DESIGN.md §11.4. Set by [Classify].
	Contributing bool

	// Outrec is non-nil iff this edge is currently a "hot" edge contributing
	// to an output ring. See DESIGN.md §12.2 and [ActiveEdge.IsHotEdge].
	Outrec *OutRec
}

// XAtY returns the X coordinate where seg crosses scanline y, rounded to the
// integer grid. For a horizontal segment it returns the segment's own X
// (Bot.X by convention) — horizontal edges aren't directly ordered by X in
// the sweep and are handled by the engine as special events.
func XAtY(seg *Segment, y fixed.Coord) fixed.Coord {
	if seg.Horizontal() {
		return seg.Bot.X
	}
	dy := float64(int64(seg.Top.Y) - int64(seg.Bot.Y))
	dx := float64(int64(seg.Top.X) - int64(seg.Bot.X))
	t := float64(int64(y)-int64(seg.Bot.Y)) / dy
	return fixed.Coord(math.Round(float64(seg.Bot.X) + t*dx))
}

// slope returns the segment's dX/dY ratio in float64 form. Used as the
// tie-breaker in the AEL ordering when two edges share the same CurrX
// (typically when they share a vertex on the scanline). A horizontal edge
// is reported as +Inf so it sorts to the right of any sloped edge sharing
// its vertex.
func slope(seg *Segment) float64 {
	if seg.Horizontal() {
		return math.Inf(+1)
	}
	dy := float64(int64(seg.Top.Y) - int64(seg.Bot.Y))
	dx := float64(int64(seg.Top.X) - int64(seg.Bot.X))
	return dx / dy
}

// AEL is the active edge list — the set of segments that cross the current
// scanline, kept ordered left-to-right by CurrX with a slope tie-breaker.
//
// This implementation uses a sorted slice: insert/remove are O(n) and
// SwapAt at a known index is O(1). For Phase 2 correctness work this is
// adequate; Phase 5 (DESIGN §7) replaces it with a balanced structure.
type AEL struct {
	edges         []*ActiveEdge
	nextOutRecIdx int
}

// NewAEL returns an empty AEL.
func NewAEL() *AEL { return &AEL{} }

// NextOutRecIdx returns a fresh sequential index for a new [OutRec]. Used by
// [AddLocalMinPoly] for deterministic ring-merge tie-breaking.
func (a *AEL) NextOutRecIdx() int {
	idx := a.nextOutRecIdx
	a.nextOutRecIdx++
	return idx
}

// Len returns the number of active edges.
func (a *AEL) Len() int { return len(a.edges) }

// At returns the active edge at position i (0 = leftmost). Out-of-range
// indices panic.
func (a *AEL) At(i int) *ActiveEdge { return a.edges[i] }

// Insert places ae into the AEL at the position determined by its CurrX
// (and slope, for ties) and returns the index it was inserted at. CurrX
// must already be set; callers typically compute it via [XAtY] for the
// current scanline.
func (a *AEL) Insert(ae *ActiveEdge) int {
	idx := sort.Search(len(a.edges), func(i int) bool {
		return aelLess(ae, a.edges[i])
	})
	a.edges = append(a.edges, nil)
	copy(a.edges[idx+1:], a.edges[idx:])
	a.edges[idx] = ae
	return idx
}

// Remove deletes ae from the AEL. It is a no-op if ae is not present.
func (a *AEL) Remove(ae *ActiveEdge) {
	for i, e := range a.edges {
		if e == ae {
			a.edges = append(a.edges[:i], a.edges[i+1:]...)
			return
		}
	}
}

// SwapAt swaps the entries at indices i and i+1. Used at an intersection
// event to reflect that two neighbouring edges have crossed.
func (a *AEL) SwapAt(i int) {
	a.edges[i], a.edges[i+1] = a.edges[i+1], a.edges[i]
}

// IndexOf returns the position of ae in the AEL, or -1 if not present.
func (a *AEL) IndexOf(ae *ActiveEdge) int {
	for i, e := range a.edges {
		if e == ae {
			return i
		}
	}
	return -1
}

// LeftOf returns the edge immediately to the left of position i, or nil if
// i is at the leftmost position.
func (a *AEL) LeftOf(i int) *ActiveEdge {
	if i <= 0 {
		return nil
	}
	return a.edges[i-1]
}

// RightOf returns the edge immediately to the right of position i, or nil
// if i is at the rightmost position.
func (a *AEL) RightOf(i int) *ActiveEdge {
	if i+1 >= len(a.edges) {
		return nil
	}
	return a.edges[i+1]
}

// UpdateForScanline recomputes CurrX for every edge given the new scanline
// Y. It does not re-sort the AEL — that is the engine's responsibility,
// because reorderings correspond to intersection events that must also
// emit output contributions.
func (a *AEL) UpdateForScanline(y fixed.Coord) {
	for _, e := range a.edges {
		e.CurrX = XAtY(e.Seg, y)
	}
}

// aelLess reports whether a sorts strictly to the left of b in the AEL.
func aelLess(a, b *ActiveEdge) bool {
	if a.CurrX != b.CurrX {
		return a.CurrX < b.CurrX
	}
	return slope(a.Seg) < slope(b.Seg)
}
