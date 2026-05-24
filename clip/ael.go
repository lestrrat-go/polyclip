package clip

import (
	"math"
	"sort"

	"github.com/lestrrat-go/polyclip/fixed"
)

// ActiveEdge tracks a single segment as it crosses the active scanline.
// In the bound model (DESIGN.md §12.1), each ActiveEdge represents one of
// two ascending bounds emerging from a local minimum; Bound/EdgeIdx
// identify the current segment within that bound, and Seg caches
// Bound.Segs[EdgeIdx].
type ActiveEdge struct {
	Seg *Segment

	// Bound is the bound this edge belongs to. nil for ActiveEdges that
	// were not created via the bound-model spawn path (legacy direct-Seg
	// callers in tests and the older per-edge sweep skeleton).
	Bound *Bound

	// EdgeIdx is the cursor position into Bound.Segs. The sweep advances it
	// (in place) when the current edge reaches its Top without ending the
	// bound — see sweep.advanceBoundCursor and sweep.doHorizontal.
	EdgeIdx int

	// CurrX is the X coordinate where Seg crosses the current scanline Y.
	// It is updated whenever the scanline advances and at intersection
	// events that reorder neighbours.
	CurrX fixed.Coord

	// WindDx is the signed input-traversal direction of this edge's bound,
	// ±1 (Clipper2's wind_dx). It is set once when the ActiveEdge is spawned
	// and never changes — every segment of a bound shares the same WindDx,
	// including leading/trailing horizontals. [Classify] uses WindDx as the
	// edge's winding contribution; this is what lets a horizontal carry its
	// bound's contribution while it sits in the AEL (DESIGN.md §12.6.1).
	WindDx int

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

// IsBoundLast reports whether ae's cursor is on the last segment of its
// bound (the local-max edge). False when ae has no bound.
func (ae *ActiveEdge) IsBoundLast() bool {
	return ae.Bound != nil && ae.EdgeIdx == len(ae.Bound.Segs)-1
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

// numXAtY returns seg.Bot.X·dy + (y − seg.Bot.Y)·dx as an exact [fixed.I128],
// the numerator of seg's X at scanline y over the denominator dy = Top.Y −
// Bot.Y. seg must be non-horizontal (dy > 0). For grid coordinates up to
// [fixed.MaxCoordMagnitude] the value fits in 128 bits.
func numXAtY(seg *Segment, y fixed.Coord) fixed.I128 {
	dy := int64(seg.Top.Y) - int64(seg.Bot.Y)
	dx := int64(seg.Top.X) - int64(seg.Bot.X)
	return fixed.MulI64(int64(seg.Bot.X), dy).Add(fixed.MulI64(int64(y)-int64(seg.Bot.Y), dx))
}

// cmpXAtY reports whether non-horizontal edge a is left of (−1), at (0), or
// right of (+1) edge b at scanline y, comparing their exact X intercepts with
// no float rounding (unlike [XAtY]). Both denominators Top.Y − Bot.Y are
// strictly positive for canonical non-horizontal segments.
func cmpXAtY(a, b *Segment, y fixed.Coord) int {
	return fixed.CmpRationals(
		numXAtY(a, y), int64(a.Top.Y)-int64(a.Bot.Y),
		numXAtY(b, y), int64(b.Top.Y)-int64(b.Bot.Y),
	)
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
	rings         []*OutRec
	nextOutRecIdx int

	// Fill is the fill rule used to classify whether an edge is contributing
	// (see [isContributing]). Zero value [FillNonZero] is the boolean-op
	// default; the offset self-union sets [FillPositive].
	Fill FillRule

	// Ordered is set by the ordered-minima self-union entry [SweepRingsFill].
	// It selects the pure-prefix-sum winding model and boundary contributing
	// test (DESIGN.md §7.2) used to resolve a self-overlapping ring's doubled
	// coincident walls. The soup-based [SweepFill] / boolean paths leave it
	// false and keep Clipper2's incremental NonZero model unchanged.
	Ordered bool
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

// RegisterRing records r in the AEL's ring registry so [AEL.Rings] returns
// it after the sweep finishes. Called by [AddLocalMinPoly].
func (a *AEL) RegisterRing(r *OutRec) {
	a.rings = append(a.rings, r)
}

// Rings returns every [OutRec] created during the sweep. Closed rings have
// non-nil Pts; merged-and-discarded rings have nil Pts and should be skipped
// by postprocess.
func (a *AEL) Rings() []*OutRec { return a.rings }

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

// InsertAt inserts ae at position idx, shifting existing entries right. Used
// by the local-min handler to place a freshly-spawned right bound adjacent to
// its left bound before bubbling it into sorted position (mirroring Clipper2's
// InsertRightEdge + the IsValidAelOrder bubble loop).
func (a *AEL) InsertAt(idx int, ae *ActiveEdge) {
	a.edges = append(a.edges, nil)
	copy(a.edges[idx+1:], a.edges[idx:])
	a.edges[idx] = ae
}

// Less reports whether edge x sorts strictly left of edge y in the AEL. It is
// the exported form of the internal ordering used by [AEL.Insert].
func (a *AEL) Less(x, y *ActiveEdge) bool { return aelLess(x, y) }

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
