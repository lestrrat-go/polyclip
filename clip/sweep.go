package clip

import "github.com/lestrrat-go/polyclip/fixed"

// Sweep runs the scanline sweep over segs and records the sequence of events
// it processed. This Phase 2 skeleton wires the event queue, active edge
// list, and intersection scheduling together; it does NOT yet compute
// classification or emit any output. The trace is the unit of testability
// for now — see DESIGN.md §11.5 for the event-handler procedures the next
// increments will fill in.
//
// segs is taken by value; callers should not mutate the slice after calling
// Sweep, because the sweep retains pointers into it.
func Sweep(segs []Segment) *SweepResult {
	s := newSweep(segs)
	s.run()
	return &SweepResult{Trace: s.trace}
}

// SweepResult is the result of [Sweep].
type SweepResult struct {
	// Trace is the sequence of events processed by the sweep, in order.
	// Tests assert on this slice; production callers ignore it.
	Trace []TraceEvent
}

// TraceEvent is one entry in [SweepResult.Trace].
type TraceEvent struct {
	Kind EventKind
	P    fixed.Point
	SegA *Segment
	SegB *Segment // populated only for EventIntersection
}

type sweep struct {
	segs  []Segment
	queue *EventQueue
	ael   *AEL
	bySeg map[*Segment]*ActiveEdge
	trace []TraceEvent
}

func newSweep(segs []Segment) *sweep {
	s := &sweep{
		segs:  segs,
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
		e := s.queue.Pop()
		s.trace = append(s.trace, TraceEvent(e))
		switch e.Kind {
		case EventBot:
			s.handleBot(e)
		case EventTop:
			s.handleTop(e)
		case EventIntersection:
			s.handleIntersection(e)
		case EventHoriz:
			s.handleHoriz(e)
		}
	}
}

func (s *sweep) handleBot(e Event) {
	ae := &ActiveEdge{Seg: e.SegA, CurrX: e.SegA.Bot.X}
	i := s.ael.Insert(ae)
	s.bySeg[e.SegA] = ae
	if left := s.ael.LeftOf(i); left != nil {
		s.maybeScheduleIntersect(left, ae, e.P.Y)
	}
	if right := s.ael.RightOf(i); right != nil {
		s.maybeScheduleIntersect(ae, right, e.P.Y)
	}
}

func (s *sweep) handleTop(e Event) {
	ae, ok := s.bySeg[e.SegA]
	if !ok {
		// Defensive: a Top fired for a segment that never entered the AEL.
		// Should not happen for valid input.
		return
	}
	i := s.ael.IndexOf(ae)
	left, right := s.ael.LeftOf(i), s.ael.RightOf(i)
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
		// One of the segments has already left the AEL: stale event.
		return
	}
	iA := s.ael.IndexOf(aeA)
	iB := s.ael.IndexOf(aeB)
	if iA < 0 || iB < 0 {
		return
	}
	if iA > iB {
		iA, iB = iB, iA
		aeA, aeB = aeB, aeA
	}
	if iB != iA+1 {
		// No longer adjacent — configuration changed since the event was
		// scheduled. Drop.
		return
	}
	// Update both edges' CurrX to the intersection X before the swap so
	// neighbour-checks below see the correct ordering.
	aeA.CurrX = e.P.X
	aeB.CurrX = e.P.X
	s.ael.SwapAt(iA)
	// After SwapAt(iA): aeB is at iA, aeA is at iA+1.
	if left := s.ael.LeftOf(iA); left != nil {
		s.maybeScheduleIntersect(left, s.ael.At(iA), e.P.Y)
	}
	if right := s.ael.RightOf(iA + 1); right != nil {
		s.maybeScheduleIntersect(s.ael.At(iA+1), right, e.P.Y)
	}
}

func (s *sweep) handleHoriz(_ Event) {
	// Horizontal-segment handling is the responsibility of increment 7
	// (output emission). For the skeleton we record the event in the trace
	// but otherwise do nothing — the AEL is not modified.
}

// maybeScheduleIntersect checks whether two AEL neighbours cross strictly
// above the current scanline Y and, if so, pushes an EventIntersection.
//
// Touches at the current scanline are intentionally ignored: the AEL
// ordering already accounts for them, and re-pushing would cause an
// infinite loop. Touches at a future scanline are pushed (they may or may
// not still be relevant when they fire).
func (s *sweep) maybeScheduleIntersect(left, right *ActiveEdge, currY fixed.Coord) {
	res := Intersect(*left.Seg, *right.Seg)
	switch res.Kind {
	case ProperCross, Touch:
		if res.P.Y <= currY {
			return
		}
		s.queue.Push(Event{
			Kind: EventIntersection,
			P:    res.P,
			SegA: left.Seg,
			SegB: right.Seg,
		})
	default:
		// NoCrossing, CollinearOverlap — nothing to do at this layer.
	}
}
