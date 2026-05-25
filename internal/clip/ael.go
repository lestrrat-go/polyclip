package clip

import (
	"math"
	"sort"

	"github.com/lestrrat-go/polyclip/internal/fixed"
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

	// RecordCrossings, set by [SweepFillZ], makes [IntersectEdges] record each
	// edge meeting it dispatches (the four endpoints + crossing point) for
	// Z-coordinate assignment. Off by default, so the standard path allocates
	// nothing extra and is bit-for-bit identical.
	RecordCrossings bool
	crossings       []ZCrossing
}

// NewAEL returns an empty AEL.
func NewAEL() *AEL { return &AEL{} }

// recordCrossing appends the meeting of e1 and e2 at pt to the crossing log,
// when recording is enabled. Called from [IntersectEdges]; see [ZCrossing].
func (a *AEL) recordCrossing(e1, e2 *ActiveEdge, pt fixed.Point) {
	if !a.RecordCrossings {
		return
	}
	s1, s2 := e1.Seg, e2.Seg
	a.crossings = append(a.crossings, ZCrossing{
		E1Bot: s1.Bot, E1Top: s1.Top,
		E2Bot: s2.Bot, E2Top: s2.Top,
		P: pt,
	})
}

// Crossings returns the recorded edge meetings (nil unless [RecordCrossings]
// was set). See [SweepFillZ].
func (a *AEL) Crossings() []ZCrossing { return a.crossings }

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
	if sa, sb := slope(a.Seg), slope(b.Seg); sa != sb {
		return sa < sb
	}
	// Coincident at this scanline (same CurrX and same slope). A static order
	// here is wrong: two exactly-coincident cross-source edges (a doubled
	// boundary, e.g. a shared vertical wall) are geometrically ordered by where
	// their bounds first DIVERGE just above the scanline — the non-degenerate
	// limit. Look ahead along both bounds to that divergence and order by which
	// path runs left there. This is what makes the coincident-edge winding
	// resolve correctly without a context-dependent guess (DESIGN.md §7.6).
	less, decided := coincidentDivergeLess(a, b)
	if decided {
		return less
	}
	return false
}

// coincidentDivergeLess orders two edges that are coincident at the current
// scanline by the first point where their bounds diverge above it. It walks
// both bounds' upward vertex paths in lockstep; at the first vertex that
// differs, the two divergence rays leave a shared vertex V, and a sorts left of
// b iff a's ray is to the left of b's (cross product < 0). Returns (less,
// decided); decided is false when a bound is unavailable or the paths never
// diverge (no basis to order — caller keeps insertion order).
func coincidentDivergeLess(a, b *ActiveEdge) (less, decided bool) {
	pa := upwardVertices(a)
	pb := upwardVertices(b)
	n := min(len(pa), len(pb))
	for k := 1; k < n; k++ {
		if pa[k] == pb[k] {
			continue
		}
		v := pa[k-1] // last common vertex (== pb[k-1])
		ax := int64(pa[k].X) - int64(v.X)
		ay := int64(pa[k].Y) - int64(v.Y)
		bx := int64(pb[k].X) - int64(v.X)
		by := int64(pb[k].Y) - int64(v.Y)
		// cross = ax*by - ay*bx in 128 bits: at the 2^60 grid the int64 product
		// overflows (a false zero), so use exact arithmetic.
		cross := fixed.MulI64(ax, by).Sub(fixed.MulI64(ay, bx)).Sign()
		if cross == 0 {
			return false, false // collinear rays: cannot order from here
		}
		// Both rays leave V into the upper half-plane (ascending bounds).
		// a runs left of b iff a's ray is counter-clockwise toward up-left,
		// i.e. cross(a,b) < 0.
		return cross < 0, true
	}
	return false, false
}

// upwardVertices returns the vertices ae's bound visits from its current
// segment upward to the bound's local maximum, in traversal order. Used by
// [coincidentDivergeLess] to compare two coincident bounds by their divergence.
// Returns nil when ae has no bound.
func upwardVertices(ae *ActiveEdge) []fixed.Point {
	if ae.Bound == nil || ae.EdgeIdx >= len(ae.Bound.Segs) {
		return nil
	}
	segs := ae.Bound.Segs[ae.EdgeIdx:]
	cur := segs[0].Bot
	// Orient the first segment so we leave from the lower (entry) end. For a
	// non-horizontal segment that is Bot; for a leading horizontal pick the end
	// that connects to the next segment as the exit.
	if len(segs) > 1 {
		n := segs[1]
		if segs[0].Bot == n.Bot || segs[0].Bot == n.Top {
			cur = segs[0].Top
		}
	}
	pts := make([]fixed.Point, 0, len(segs)+2)
	pts = append(pts, cur)
	for _, s := range segs {
		nxt := s.Top
		if s.Top == cur {
			nxt = s.Bot
		}
		pts = append(pts, nxt)
		cur = nxt
	}
	// The bound ends at a local maximum (cur). The ring turns there toward this
	// bound's maxima partner: a LEFT bound (interior to the right, WindDx < 0)
	// turns right, a RIGHT bound (interior to the left, WindDx > 0) turns left.
	// Append that turn direction so two bounds coincident up to a max still order
	// correctly when one tops out while the other continues straight up
	// (DESIGN.md §7.6).
	if ae.WindDx < 0 {
		pts = append(pts, fixed.Point{X: cur.X + 1, Y: cur.Y})
	} else if ae.WindDx > 0 {
		pts = append(pts, fixed.Point{X: cur.X - 1, Y: cur.Y})
	}
	return pts
}
