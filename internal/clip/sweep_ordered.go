package clip

import (
	"sort"

	"github.com/lestrrat-go/polyclip/internal/fixed"
)

// SweepRingsFill runs the sweep over rings given in per-ring traversal order
// (within each ring, segment i's End() == segment (i+1)'s Start(), wrapping).
// It builds local minima per ring with traversal order PRESERVED, splitting
// each ordered segment only at the endpoints introduced by collinear-overlap
// and T-junction preprocessing — it never reconstructs ring connectivity from
// a segment soup the way [BuildLocalMinima] does.
//
// This is required for a self-overlapping ring whose inward offset folds a
// wall onto itself: the doubled coincident wall makes collinear degree-4
// vertices that the soup walker ([traceRing]) cannot disambiguate (all the
// candidate continuations are identical), but in traversal order the two
// wall passes occupy distinct sequence positions and resolve cleanly. Used
// only by the positive-fill self-union (offset.go); see DESIGN.md §7.2.
//
// Transversal self-crossings are left to the sweep's [doIntersections]; only
// collinear overlaps and T-junctions are pre-split here.
func SweepRingsFill(orderedRings [][]Segment, op Operation, fill FillRule) *SweepResult {
	flat, ringSpans := splitOrderedRings(orderedRings)

	var minima []LocalMin
	for _, span := range ringSpans {
		ptrs := make([]*Segment, 0, span.hi-span.lo)
		for i := span.lo; i < span.hi; i++ {
			if flat[i].Degenerate() {
				continue
			}
			ptrs = append(ptrs, &flat[i])
		}
		if len(ptrs) == 0 {
			continue
		}
		mins, err := findRingMinima(ptrs)
		if err != nil {
			return &SweepResult{Err: err}
		}
		minima = append(minima, mins...)
	}
	sort.Slice(minima, func(i, j int) bool {
		return LessYX(minima[i].Vertex, minima[j].Vertex)
	})

	s := newSweepFromMinima(flat, op, fill, minima)
	s.run()
	return &SweepResult{Trace: s.trace, Rings: s.ael.Rings(), Err: s.err}
}

type ringSpan struct{ lo, hi int }

// splitOrderedRings splits every ordered segment at the interior endpoints
// produced by [SplitOverlaps]/[SplitTJunctions] over the flattened soup, while
// keeping each ring's segments contiguous and in traversal order. It returns
// the flattened split segments and the [lo,hi) span of each ring within them.
func splitOrderedRings(orderedRings [][]Segment) ([]Segment, []ringSpan) {
	// Collect the full split-point set from the order-agnostic passes.
	var soup []Segment
	for _, r := range orderedRings {
		soup = append(soup, r...)
	}
	soup = SplitOverlaps(soup)
	soup = SplitTJunctions(soup)
	points := make(map[fixed.Point]struct{}, 2*len(soup))
	for i := range soup {
		points[soup[i].Bot] = struct{}{}
		points[soup[i].Top] = struct{}{}
	}

	var flat []Segment
	spans := make([]ringSpan, 0, len(orderedRings))
	for _, r := range orderedRings {
		lo := len(flat)
		for i := range r {
			flat = appendSplitSegment(flat, r[i], points)
		}
		spans = append(spans, ringSpan{lo: lo, hi: len(flat)})
	}
	return flat, spans
}

// appendSplitSegment appends seg to flat in input-traversal direction, cut at
// every point of pts that lies strictly in its interior. The emitted pieces
// preserve seg.Start()→seg.End() direction so the ring stays ordered.
func appendSplitSegment(flat []Segment, seg Segment, pts map[fixed.Point]struct{}) []Segment {
	start, end := seg.Start(), seg.End()
	var cuts []fixed.Point
	for p := range pts {
		if p == start || p == end {
			continue
		}
		if fixed.Orient2D(seg.Bot, seg.Top, p) != 0 {
			continue
		}
		if !onCollinearSegment(seg, p) {
			continue
		}
		cuts = append(cuts, p)
	}
	if len(cuts) == 0 {
		if !seg.Degenerate() {
			flat = append(flat, seg)
		}
		return flat
	}
	// Order cuts along start→end.
	sort.Slice(cuts, func(i, j int) bool {
		return distSq(start, cuts[i]) < distSq(start, cuts[j])
	})
	prev := start
	for _, c := range cuts {
		s := NewSegment(prev, c, seg.Src)
		if !s.Degenerate() {
			flat = append(flat, s)
		}
		prev = c
	}
	s := NewSegment(prev, end, seg.Src)
	if !s.Degenerate() {
		flat = append(flat, s)
	}
	return flat
}

func distSq(a, b fixed.Point) float64 {
	dx := float64(int64(b.X) - int64(a.X))
	dy := float64(int64(b.Y) - int64(a.Y))
	return dx*dx + dy*dy
}

// newSweepFromMinima builds a bound-model sweep seeded with pre-computed local
// minima (from [SweepRingsFill]) rather than [BuildLocalMinima]. Every segment
// of flat is claimed by a bound, so the per-segment event fallback is skipped.
func newSweepFromMinima(flat []Segment, op Operation, fill FillRule, minima []LocalMin) *sweep {
	s := &sweep{
		segs:       flat,
		op:         op,
		queue:      NewEventQueue(),
		ael:        NewAEL(),
		bySeg:      make(map[*Segment]*ActiveEdge, len(flat)),
		minima:     make(map[fixed.Point]*LocalMin, len(minima)),
		boundModel: true,
	}
	s.ael.Fill = fill
	s.ael.Ordered = true
	for i := range minima {
		lm := &minima[i]
		s.minima[lm.Vertex] = lm
		s.queue.Push(Event{Kind: EventLocalMin, P: lm.Vertex, LocalMin: lm})
	}
	return s
}
