package clip

import "github.com/lestrrat-go/polyclip/fixed"

// Sweep runs the scanline sweep over segs for the given boolean operation
// and returns both a trace of processed events and the constructed output
// rings. See DESIGN.md §11.5 and §12.5 / §12.6 / §12.7 for the algorithm.
//
// segs is taken by value; callers should not mutate the slice after calling
// Sweep, because the sweep retains pointers into it.
//
// Limitation: input segments must not be horizontal. Horizontal handling is
// implemented separately at the caller level for now (see DESIGN.md §12.6).
// Horizontal segments are skipped in this Sweep — they fire EventHoriz but
// the handler is a no-op.
func Sweep(segs []Segment, op Operation) *SweepResult {
	s := newSweep(segs, op)
	s.run()
	return &SweepResult{Trace: s.trace, Rings: s.ael.Rings()}
}

// SweepResult is the result of [Sweep].
type SweepResult struct {
	// Trace is the sequence of events processed by the sweep, in order.
	// Tests assert on this slice; production callers ignore it.
	Trace []TraceEvent

	// Rings holds every [OutRec] created during the sweep. Closed rings have
	// non-nil Pts; rings that were merged into another have Pts == nil and
	// must be filtered out by postprocess.
	Rings []*OutRec
}

// TraceEvent is one entry in [SweepResult.Trace]. The WindSelf, WindOther
// and Contributing fields capture the classification snapshot of SegA at the
// moment its event was processed (zero for events that don't classify, such
// as Top events removing edges from the AEL).
type TraceEvent struct {
	Kind         EventKind
	P            fixed.Point
	SegA         *Segment
	SegB         *Segment // populated only for EventIntersection
	WindSelf     int
	WindOther    int
	Contributing bool
}

type sweep struct {
	segs  []Segment
	op    Operation
	queue *EventQueue
	ael   *AEL
	bySeg map[*Segment]*ActiveEdge
	trace []TraceEvent
}

func newSweep(segs []Segment, op Operation) *sweep {
	s := &sweep{
		segs:  segs,
		op:    op,
		queue: NewEventQueue(),
		ael:   NewAEL(),
		bySeg: make(map[*Segment]*ActiveEdge, len(segs)),
	}
	for i := range segs {
		seg := &s.segs[i]
		if seg.Degenerate() {
			continue
		}
		if seg.Horizontal() {
			s.queue.Push(Event{Kind: EventHoriz, P: seg.Bot, SegA: seg})
			continue
		}
		s.queue.Push(Event{Kind: EventBot, P: seg.Bot, SegA: seg})
		s.queue.Push(Event{Kind: EventTop, P: seg.Top, SegA: seg})
	}
	return s
}

func (s *sweep) run() {
	for s.queue.Len() > 0 {
		first := s.queue.Pop()
		batch := []Event{first}
		// Collect every event sharing this point. Within a batch the
		// EventKind ordering (Top < Horiz < Bot < Intersection) is already
		// preserved by the heap.
		for s.queue.Len() > 0 && s.queue.Peek().P == first.P {
			batch = append(batch, s.queue.Pop())
		}
		s.handleBatch(batch)
	}
}

// handleBatch dispatches based on the composition of simultaneous events at
// a single (Y, X). Recognised configurations:
//
//   - 2 Tops + 0 Bots same source → local maximum.
//   - 0 Tops + 2 Bots same source → local minimum.
//   - 1 Top + 1 Bot same source   → through-vertex (bound continues).
//   - 1 Intersection                → IntersectEdges dispatcher.
//
// Mixed configurations (multiple sources at one point, intersections
// coinciding with endpoints) fall back to per-event processing — output may
// be wrong in those cases. They are explicitly addressed by a later
// increment.
func (s *sweep) handleBatch(batch []Event) {
	tops := []Event{}
	bots := []Event{}
	for _, e := range batch {
		switch e.Kind {
		case EventTop:
			tops = append(tops, e)
		case EventBot:
			bots = append(bots, e)
		case EventIntersection:
			s.handleIntersection(e)
			s.appendTrace(e, nil)
		case EventHoriz:
			s.appendTrace(e, nil)
		}
	}

	switch {
	case len(tops) == 1 && len(bots) == 1 && tops[0].SegA.Src == bots[0].SegA.Src:
		s.handleThroughVertex(tops[0], bots[0])
	case len(tops) == 2 && len(bots) == 0:
		s.handleLocalMaximum(tops[0], tops[1])
	case len(tops) == 0 && len(bots) == 2:
		s.handleLocalMinimum(bots[0], bots[1])
	case len(tops) == 0 && len(bots) == 1:
		ae := s.handleBot(bots[0])
		s.appendTrace(bots[0], ae)
	case len(tops) == 1 && len(bots) == 0:
		s.handleTop(tops[0])
		s.appendTrace(tops[0], nil)
	default:
		// Fallback: process individually. Output rings may be wrong for
		// configurations not handled above.
		for _, e := range tops {
			s.handleTop(e)
			s.appendTrace(e, nil)
		}
		for _, e := range bots {
			ae := s.handleBot(e)
			s.appendTrace(e, ae)
		}
	}
}

func (s *sweep) appendTrace(e Event, ae *ActiveEdge) {
	te := TraceEvent{Kind: e.Kind, P: e.P, SegA: e.SegA, SegB: e.SegB}
	if ae != nil {
		te.WindSelf, te.WindOther, te.Contributing = ae.WindSelf, ae.WindOther, ae.Contributing
	}
	s.trace = append(s.trace, te)
}

// handleLocalMinimum is the standard Vatti local-min handler: two new edges
// emerging upward from the same vertex form the two sides of a new
// contributing ring (if the classification says so).
func (s *sweep) handleLocalMinimum(b1, b2 Event) {
	ae1 := s.handleBot(b1)
	ae2 := s.handleBot(b2)
	s.appendTrace(b1, ae1)
	s.appendTrace(b2, ae2)
	if ae1.Contributing && ae2.Contributing {
		AddLocalMinPoly(s.ael, ae1, ae2, b1.P, true)
	}
}

// handleLocalMaximum closes the two AEL edges meeting at a top vertex,
// emitting AddLocalMaxPoly if both were contributing.
func (s *sweep) handleLocalMaximum(t1, t2 Event) {
	ae1, ok1 := s.bySeg[t1.SegA]
	ae2, ok2 := s.bySeg[t2.SegA]
	if !ok1 || !ok2 {
		// Fallback per-event.
		s.handleTop(t1)
		s.handleTop(t2)
		s.appendTrace(t1, nil)
		s.appendTrace(t2, nil)
		return
	}
	if ae1.Contributing && ae2.Contributing {
		AddLocalMaxPoly(s.ael, ae1, ae2, t1.P)
	}
	s.ael.Remove(ae1)
	s.ael.Remove(ae2)
	delete(s.bySeg, t1.SegA)
	delete(s.bySeg, t2.SegA)
	s.appendTrace(t1, ae1)
	s.appendTrace(t2, ae2)
}

// handleThroughVertex updates a single AEL entry's segment when one input
// edge ends at a vertex that is not a local maximum (the polygon "continues"
// upward into the next edge of the same bound).
func (s *sweep) handleThroughVertex(top Event, bot Event) {
	ae, ok := s.bySeg[top.SegA]
	if !ok {
		// Fall back.
		s.handleTop(top)
		s.handleBot(bot)
		s.appendTrace(top, nil)
		s.appendTrace(bot, nil)
		return
	}
	delete(s.bySeg, top.SegA)
	ae.Seg = bot.SegA
	ae.CurrX = bot.P.X
	s.bySeg[bot.SegA] = ae

	// If this contributing edge is hot, emit an OutPt at the vertex so the
	// ring traces the polygon corner.
	if ae.Contributing && ae.IsHotEdge() {
		AddOutPt(ae, top.P)
	}

	i := s.ael.IndexOf(ae)
	if left := s.ael.LeftOf(i); left != nil {
		s.maybeScheduleIntersect(left, ae, top.P.Y)
	}
	if right := s.ael.RightOf(i); right != nil {
		s.maybeScheduleIntersect(ae, right, top.P.Y)
	}
	s.appendTrace(top, ae)
	s.appendTrace(bot, ae)
}

func (s *sweep) handleBot(e Event) *ActiveEdge {
	ae := &ActiveEdge{Seg: e.SegA, CurrX: e.SegA.Bot.X}
	i := s.ael.Insert(ae)
	s.bySeg[e.SegA] = ae
	Classify(s.ael, ae, s.op)
	if left := s.ael.LeftOf(i); left != nil {
		s.maybeScheduleIntersect(left, ae, e.P.Y)
	}
	if right := s.ael.RightOf(i); right != nil {
		s.maybeScheduleIntersect(ae, right, e.P.Y)
	}
	return ae
}

func (s *sweep) handleTop(e Event) {
	ae, ok := s.bySeg[e.SegA]
	if !ok {
		return
	}
	i := s.ael.IndexOf(ae)
	left, right := s.ael.LeftOf(i), s.ael.RightOf(i)
	if ae.Contributing && ae.IsHotEdge() {
		AddOutPt(ae, e.P)
	}
	s.ael.Remove(ae)
	delete(s.bySeg, e.SegA)
	if left != nil && right != nil {
		s.maybeScheduleIntersect(left, right, e.P.Y)
	}
}

func (s *sweep) handleIntersection(e Event) {
	aeA, okA := s.bySeg[e.SegA]
	aeB, okB := s.bySeg[e.SegB]
	if !okA || !okB {
		return
	}
	IntersectEdges(s.ael, s.op, aeA, aeB, e.P)
	// IntersectEdges already swapped, re-classified, and emitted output.
	// Schedule fresh intersection checks for the (newly) adjacent neighbours.
	iA := s.ael.IndexOf(aeA)
	if iA < 0 {
		return
	}
	if left := s.ael.LeftOf(iA); left != nil {
		s.maybeScheduleIntersect(left, s.ael.At(iA), e.P.Y)
	}
	if right := s.ael.RightOf(iA); right != nil {
		s.maybeScheduleIntersect(s.ael.At(iA), right, e.P.Y)
	}
}

func (s *sweep) maybeScheduleIntersect(left, right *ActiveEdge, currY fixed.Coord) {
	res := Intersect(*left.Seg, *right.Seg)
	if res.Kind != ProperCross {
		// A Touch at an endpoint is the local-min/max event for that vertex
		// and is processed by the corresponding Top/Bot batch; scheduling it
		// as an intersection would double-handle it. A CollinearOverlap is
		// preprocessed away (or, if it slips through, the sweep cannot
		// disambiguate it here). NoCrossing is the common case.
		return
	}
	if res.P.Y <= currY {
		return
	}
	s.queue.Push(Event{
		Kind: EventIntersection,
		P:    res.P,
		SegA: left.Seg,
		SegB: right.Seg,
	})
}
