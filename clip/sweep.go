package clip

import "github.com/lestrrat-go/polyclip/fixed"

// Sweep runs the scanline sweep over segs for the given boolean operation
// and returns both a trace of processed events and the constructed output
// rings. See DESIGN.md §11.5 and §12.5 / §12.6 for the algorithm.
//
// segs is taken by value; callers should not mutate the slice after calling
// Sweep, because the sweep retains pointers into it.
//
// Horizontal-segment support: axial local-minimum and local-maximum
// horizontals are handled per §12.6 by classifying them in a pre-pass and
// scheduling [EventHoriz] / [EventHorizMaxOpen] events. Mid-bound
// horizontals (staircases) are not yet supported; their presence produces
// a SweepResult whose Err field is set.
func Sweep(segs []Segment, op Operation) *SweepResult {
	s := newSweep(segs, op)
	if s.err != nil {
		return &SweepResult{Err: s.err}
	}
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

	// Err is non-nil if the sweep aborted before processing — currently
	// only when [ClassifyHorizontals] rejects a mid-bound horizontal.
	Err error
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
	segs   []Segment
	op     Operation
	queue  *EventQueue
	ael    *AEL
	bySeg  map[*Segment]*ActiveEdge
	horiz  map[*Segment]*HorizInfo
	minima map[fixed.Point]*LocalMin
	trace  []TraceEvent
	err    error
}

func newSweep(segs []Segment, op Operation) *sweep {
	s := &sweep{
		segs:   segs,
		op:     op,
		queue:  NewEventQueue(),
		ael:    NewAEL(),
		bySeg:  make(map[*Segment]*ActiveEdge, len(segs)),
		minima: make(map[fixed.Point]*LocalMin),
	}
	// Try the bound-model pre-pass first (DESIGN.md §12.7 / §12.10). On
	// success it claims every segment of every bound — handleLocalMin
	// spawns at minima, advanceBoundCursor advances through mid-bound
	// horizontals, closeBound closes at local maxima. ClassifyHorizontals
	// is NOT called in this path: the bound model subsumes horizontal
	// handling, including mid-bound (staircase) horizontals that the
	// strict ClassifyHorizontals rejects.
	//
	// On BLM failure (open-chain inputs, shared vertices, etc.) fall back
	// to the legacy per-edge dispatch with strict ClassifyHorizontals.
	claimed := make(map[*Segment]struct{})
	mins, mErr := BuildLocalMinima(s.segs)
	if mErr == nil {
		for i := range mins {
			lm := &mins[i]
			s.minima[lm.Vertex] = lm
			s.queue.Push(Event{Kind: EventLocalMin, P: lm.Vertex, LocalMin: lm})
			claimAllSegments(claimed, lm.Left)
			claimAllSegments(claimed, lm.Right)
		}
	} else {
		hinfo, hErr := ClassifyHorizontals(s.segs)
		if hErr != nil {
			s.err = hErr
			return s
		}
		s.horiz = hinfo
	}

	for i := range segs {
		seg := &s.segs[i]
		if seg.Degenerate() {
			continue
		}
		if _, isClaimed := claimed[seg]; isClaimed {
			// Bound model owns every event for this segment.
			continue
		}
		if seg.Horizontal() {
			info := s.horiz[seg]
			switch info.Class {
			case HorizClassMin, HorizClassUnknown:
				s.queue.Push(Event{Kind: EventHoriz, P: seg.Top, SegA: seg})
			case HorizClassMax:
				s.queue.Push(Event{Kind: EventHorizMaxOpen, P: seg.Bot, SegA: seg})
			}
			continue
		}
		s.queue.Push(Event{Kind: EventBot, P: seg.Bot, SegA: seg})
		s.queue.Push(Event{Kind: EventTop, P: seg.Top, SegA: seg})
	}
	return s
}

// claimAllSegments marks every segment of b as "claimed" by the bound
// model. Per-segment event scheduling for these segments is suppressed
// in newSweep — handleLocalMin spawns, handleTop advances the cursor and
// closes via §12.10's in-place protocol.
func claimAllSegments(claimed map[*Segment]struct{}, b *Bound) {
	if b == nil {
		return
	}
	for _, seg := range b.Segs {
		claimed[seg] = struct{}{}
	}
}

func (s *sweep) run() {
	for s.queue.Len() > 0 {
		first := s.queue.Pop()
		batch := []Event{first}
		// Collect every event sharing this point. Within a batch the
		// EventKind ordering (HorizMaxOpen < Top < Bot < Horiz <
		// Intersection) is already preserved by the heap.
		for s.queue.Len() > 0 && s.queue.Peek().P == first.P {
			batch = append(batch, s.queue.Pop())
		}
		s.handleBatch(batch)
	}
}

// handleBatch dispatches based on the composition of simultaneous events at
// a single (Y, X). Recognised configurations for Top+Bot:
//
//   - 2 Tops + 0 Bots same source → local maximum.
//   - 0 Tops + 2 Bots same source → local minimum.
//   - 1 Top + 1 Bot same source   → through-vertex (bound continues).
//   - 1 Intersection                → IntersectEdges dispatcher.
//
// Horizontal events fire on their own: [EventHorizMaxOpen] handles a
// local-max horizontal (closes a ring whose two ascending bounds reach their
// top at the horizontal's endpoints); [EventHoriz] handles a local-min
// horizontal (spawns a ring whose two ascending bounds emerge from the
// horizontal's endpoints). They are dispatched per-event rather than
// batched.
//
// Mixed configurations (multiple sources at one point, intersections
// coinciding with endpoints) fall back to per-event processing — output may
// be wrong in those cases. They are explicitly addressed by a later
// increment.
func (s *sweep) handleBatch(batch []Event) {
	var horizMaxOpens, tops, bots, localMins, horizMins, intersects []Event
	for _, e := range batch {
		switch e.Kind {
		case EventHorizMaxOpen:
			horizMaxOpens = append(horizMaxOpens, e)
		case EventTop:
			tops = append(tops, e)
		case EventBot:
			bots = append(bots, e)
		case EventLocalMin:
			localMins = append(localMins, e)
		case EventHoriz:
			horizMins = append(horizMins, e)
		case EventIntersection:
			intersects = append(intersects, e)
		}
	}

	// Phase 1: local-max horizontals close their rings while the two
	// adjacent verticals are still in the AEL.
	for _, e := range horizMaxOpens {
		s.handleHorizMax(e)
		s.appendTrace(e, nil)
	}

	// Phase 2: regular Top/Bot dispatch by configuration.
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

	// Phase 3: local minima spawn bounds via the bound-model handler.
	// Runs after any per-segment Bots at the same point so the AEL is
	// fully populated (no double-insert: claimed segments' Bots were
	// skipped in newSweep).
	for _, e := range localMins {
		s.handleLocalMin(e)
		s.appendTrace(e, nil)
	}

	// Phase 4: local-min horizontals spawn rings (legacy non-bound path —
	// fires only for HorizClassMin/Unknown horizontals not claimed by a
	// bound, i.e. when BuildLocalMinima failed).
	for _, e := range horizMins {
		s.handleHorizMin(e)
		s.appendTrace(e, nil)
	}

	// Phase 5: intersections last.
	for _, e := range intersects {
		s.handleIntersection(e)
		s.appendTrace(e, nil)
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
//
// When the bound-model pre-pass identified this vertex as a local minimum
// (s.minima lookup hit), AddLocalMinPoly is called with arguments oriented
// so the Right bound becomes the FrontEdge — matching the established
// convention in [handleHorizMin] and the existing diamond ring-direction.
// Without the pre-pass info the call falls back to heap-order (ae1, ae2),
// which is what the original code did.
func (s *sweep) handleLocalMinimum(b1, b2 Event) {
	ae1 := s.handleBot(b1)
	ae2 := s.handleBot(b2)
	// Re-classify both edges: when two new edges enter the AEL at the
	// same point, the first one was classified WITHOUT the second's
	// presence — but after both are inserted, the second sits between
	// the first and its prior left neighbour (or vice versa, depending on
	// slope), so the first edge's WindSelf/WindOther may now be stale.
	// Re-running Classify against the final AEL state restores correctness.
	Classify(s.ael, ae1, s.op)
	Classify(s.ael, ae2, s.op)
	s.appendTrace(b1, ae1)
	s.appendTrace(b2, ae2)
	if !ae1.Contributing || !ae2.Contributing {
		return
	}
	if lm := s.minima[b1.P]; lm != nil {
		// Orient so the Right bound's ActiveEdge becomes the FrontEdge of
		// the new OutRec (matching [handleHorizMin]). The Right bound's
		// first non-horizontal segment is the one that sits to the RIGHT
		// in the AEL at the local-min scanline.
		rightSeg := firstNonHorizontal(lm.Right)
		leftSeg := firstNonHorizontal(lm.Left)
		switch {
		case ae1.Seg == rightSeg && ae2.Seg == leftSeg:
			AddLocalMinPoly(s.ael, ae1, ae2, b1.P, true)
			return
		case ae2.Seg == rightSeg && ae1.Seg == leftSeg:
			AddLocalMinPoly(s.ael, ae2, ae1, b1.P, true)
			return
		}
	}
	// Fallback: heap-order — existing behavior for cases the pre-pass
	// doesn't cover (open chains, segments not in any reconstructed ring).
	AddLocalMinPoly(s.ael, ae1, ae2, b1.P, true)
}

func firstNonHorizontal(b *Bound) *Segment {
	if b == nil {
		return nil
	}
	for _, s := range b.Segs {
		if !s.Horizontal() {
			return s
		}
	}
	return nil
}

// handleLocalMin spawns the two ascending bounds of a local minimum into
// the AEL. Replaces the per-segment 2-Bot batched [sweep.handleLocalMinimum]
// for inputs that form closed rings (where [BuildLocalMinima] succeeds).
// Per DESIGN.md §12.1 / §12.7.
//
// Sequence: insert both bound entries, re-classify (fixes stale winding
// counts from the first-inserted edge), call [AddLocalMinPoly] with
// (right, left) ordering so FrontEdge = Right bound (matching the existing
// orientation convention), then emit OutPts for the far ends of any leading
// horizontals — these are vertices the ring must touch on its way up from
// the local-min vertex to the first non-horizontal AEL position.
func (s *sweep) handleLocalMin(e Event) {
	lm := e.LocalMin
	if lm == nil {
		return
	}
	leftAE := s.spawnBoundActive(lm.Left)
	rightAE := s.spawnBoundActive(lm.Right)
	if leftAE == nil || rightAE == nil {
		return
	}
	Classify(s.ael, leftAE, s.op)
	Classify(s.ael, rightAE, s.op)
	if !leftAE.Contributing || !rightAE.Contributing {
		return
	}
	AddLocalMinPoly(s.ael, rightAE, leftAE, lm.Vertex, true)
	// Leading horizontals: if a bound's first non-horizontal segment does
	// not start at the local-min vertex, the bound traversed one or more
	// horizontals from the vertex to that segment's Bot. Emit the segment's
	// Bot as a ring vertex so the horizontal endpoint isn't lost.
	if rightAE.Seg.Bot != lm.Vertex {
		AddOutPt(rightAE, rightAE.Seg.Bot)
	}
	if leftAE.Seg.Bot != lm.Vertex {
		AddOutPt(leftAE, leftAE.Seg.Bot)
	}
	// Schedule first EventTop for each bound (lazy scheduling per §12.10.4).
	// Subsequent EventTops are scheduled by advanceBoundCursor as the cursor
	// walks the bound's Segs.
	s.queue.Push(Event{Kind: EventTop, P: leftAE.Seg.Top, SegA: leftAE.Seg})
	s.queue.Push(Event{Kind: EventTop, P: rightAE.Seg.Top, SegA: rightAE.Seg})
	// Schedule intersection checks with new AEL neighbours.
	iL := s.ael.IndexOf(leftAE)
	if iL >= 0 {
		if left := s.ael.LeftOf(iL); left != nil {
			s.maybeScheduleIntersect(left, leftAE, lm.Vertex.Y)
		}
	}
	iR := s.ael.IndexOf(rightAE)
	if iR >= 0 {
		if right := s.ael.RightOf(iR); right != nil {
			s.maybeScheduleIntersect(rightAE, right, lm.Vertex.Y)
		}
	}
}

// spawnBoundActive creates an [ActiveEdge] for bound b at the local-min
// scanline. Advances past any leading horizontals to the first non-
// horizontal segment, inserts into the AEL at that segment's Bot.X, and
// returns the active edge. Returns nil if b is all-horizontal (shouldn't
// happen for a valid ring).
func (s *sweep) spawnBoundActive(b *Bound) *ActiveEdge {
	if b == nil {
		return nil
	}
	edgeIdx := 0
	for edgeIdx < len(b.Segs) && b.Segs[edgeIdx].Horizontal() {
		edgeIdx++
	}
	if edgeIdx >= len(b.Segs) {
		return nil
	}
	seg := b.Segs[edgeIdx]
	ae := &ActiveEdge{
		Seg:     seg,
		Bound:   b,
		EdgeIdx: edgeIdx,
		CurrX:   seg.Bot.X,
	}
	s.ael.Insert(ae)
	s.bySeg[seg] = ae
	return ae
}

// handleLocalMaximum closes the two AEL edges meeting at a top vertex,
// emitting AddLocalMaxPoly if both are hot (assigned to an OutRec).
// Contributing is not the right check here: at intersected/overlapping
// inputs, an edge may have Contributing=false after a post-swap
// reclassification yet still belong to a hot OutRec that needs to be
// joined/closed at this vertex. IsHotEdge captures membership in a ring,
// which is what AddLocalMaxPoly needs.
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
	if ae1.IsHotEdge() && ae2.IsHotEdge() {
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

	// Keep bound cursor consistent: if this edge belongs to a bound, find
	// bot.SegA in the bound's Segs and update EdgeIdx so future bound-aware
	// dispatch (planned for handleTop / handleLocalMaximum) sees the right
	// position.
	if ae.Bound != nil {
		for i, seg := range ae.Bound.Segs {
			if seg == bot.SegA {
				ae.EdgeIdx = i
				break
			}
		}
	}

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

	// Bound-model path (§12.10.4 / §12.10.5).
	if ae.Bound != nil {
		if ae.IsBoundLast() {
			s.closeBound(ae, nil, e.P.Y, true)
			return
		}
		s.advanceBoundCursor(ae, e.P)
		return
	}

	// Legacy path: per-segment Top with no bound info.
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

// advanceBoundCursor advances ae's bound cursor past one or more horizontals
// to the next non-horizontal segment, emitting OutPts at horizontal endpoints
// along the way. Per DESIGN.md §12.10.4 the update is IN PLACE — the AE
// keeps its AEL position (mirroring Clipper2's UpdateEdgeIntoAEL at
// engine.cpp:1731). Schedules the next EventTop for the new current edge.
//
// If the run of horizontals reaches the end of the bound (trailing
// horizontals at a local max), delegates to closeBound.
func (s *sweep) advanceBoundCursor(ae *ActiveEdge, currentTop fixed.Point) {
	if ae.Contributing && ae.IsHotEdge() {
		AddOutPt(ae, currentTop)
	}
	b := ae.Bound
	next := ae.EdgeIdx + 1
	var horizontals []*Segment
	for next < len(b.Segs) && b.Segs[next].Horizontal() {
		horizontals = append(horizontals, b.Segs[next])
		next++
	}
	if next >= len(b.Segs) {
		// advanceBoundCursor already emitted ae.Seg.Top — closeBound
		// should not re-emit.
		s.closeBound(ae, horizontals, currentTop.Y, false)
		return
	}
	// Mid-bound: emit at each horizontal's far endpoint in bound traversal
	// direction.
	for _, h := range horizontals {
		if ae.Contributing && ae.IsHotEdge() {
			AddOutPt(ae, fixed.Point{X: boundHorizontalFarX(b, h), Y: currentTop.Y})
		}
	}
	// IN-PLACE update: do NOT remove/reinsert in the AEL. The slope may
	// change but AEL ordering is fixed by the next scanbeam's intersection
	// pass (§12.10.1).
	delete(s.bySeg, ae.Seg)
	ae.EdgeIdx = next
	ae.Seg = b.Segs[next]
	ae.CurrX = ae.Seg.Bot.X
	s.bySeg[ae.Seg] = ae
	s.queue.Push(Event{Kind: EventTop, P: ae.Seg.Top, SegA: ae.Seg})
	// Schedule intersection checks against the new segment's slope: the
	// previous segment's crossings have been processed but the new edge
	// may cross neighbours that the old one didn't.
	i := s.ael.IndexOf(ae)
	if i >= 0 {
		if left := s.ael.LeftOf(i); left != nil {
			s.maybeScheduleIntersect(left, ae, currentTop.Y)
		}
		if right := s.ael.RightOf(i); right != nil {
			s.maybeScheduleIntersect(ae, right, currentTop.Y)
		}
	}
}

// closeBound closes the ring at a local maximum. Per DESIGN.md §12.10.5.
// trailingHorizontals is the list of trailing horizontals advanced through
// (nil for the no-trailing case where ae's last bound segment is non-
// horizontal).
//
// emitTopFirst controls whether to emit ae.Seg.Top before the rest of the
// trailing-horizontal walk. handleTop callers pass true (no prior emit);
// advanceBoundCursor passes false (it already emitted the Top).
func (s *sweep) closeBound(ae *ActiveEdge, trailingHorizontals []*Segment, y fixed.Coord, emitTopFirst bool) {
	if emitTopFirst && ae.Contributing && ae.IsHotEdge() {
		AddOutPt(ae, ae.Seg.Top)
	}
	// Emit far ends of intermediate trailing horizontals (all but the
	// last — the last's far end is the local-max vertex and is emitted
	// once via AddLocalMaxPoly or by the symmetric partner).
	if len(trailingHorizontals) > 1 {
		for _, hh := range trailingHorizontals[:len(trailingHorizontals)-1] {
			if ae.Contributing && ae.IsHotEdge() {
				AddOutPt(ae, fixed.Point{X: boundHorizontalFarX(ae.Bound, hh), Y: y})
			}
		}
	}
	// Resolve local-max vertex.
	var maxPt fixed.Point
	if len(trailingHorizontals) > 0 {
		h := trailingHorizontals[len(trailingHorizontals)-1]
		maxPt = fixed.Point{X: boundHorizontalFarX(ae.Bound, h), Y: y}
	} else {
		maxPt = ae.Seg.Top
	}

	// Find partner via OutRec.
	var partner *ActiveEdge
	if ae.Outrec != nil {
		if ae.Outrec.FrontEdge != nil && ae.Outrec.FrontEdge != ae {
			partner = ae.Outrec.FrontEdge
		} else if ae.Outrec.BackEdge != nil && ae.Outrec.BackEdge != ae {
			partner = ae.Outrec.BackEdge
		}
	}

	// Case A: partner doesn't exist or isn't at its bound's last. Emit
	// maxPt on ae's chain to capture the local-max vertex in the ring;
	// remove ae but leave Outrec.FrontEdge / BackEdge intact so the
	// partner's eventual closeBound can detect "ae already finished."
	if partner == nil || !partner.IsBoundLast() {
		if ae.Contributing && ae.IsHotEdge() {
			AddOutPt(ae, maxPt)
		}
		s.ael.Remove(ae)
		delete(s.bySeg, ae.Seg)
		return
	}

	// Case B: partner at end but already removed (Case A on partner's
	// earlier closeBound). The local-max vertex was emitted by partner;
	// just close the ring without re-emitting.
	if s.ael.IndexOf(partner) < 0 {
		outrec := ae.Outrec
		if outrec != nil {
			outrec.FrontEdge = nil
			outrec.BackEdge = nil
		}
		ae.Outrec = nil
		partner.Outrec = nil
		s.ael.Remove(ae)
		delete(s.bySeg, ae.Seg)
		return
	}

	// Case C: symmetric — both at end, both in AEL. AddLocalMaxPoly closes
	// with FrontEdge passed first by convention so the local-max vertex
	// prepends to Pts.
	front, back := partner, ae
	if ae.IsFront() {
		front, back = ae, partner
	}
	if front.Contributing && front.IsHotEdge() && back.Contributing && back.IsHotEdge() {
		AddLocalMaxPoly(s.ael, front, back, maxPt)
	}
	s.ael.Remove(ae)
	s.ael.Remove(partner)
	delete(s.bySeg, ae.Seg)
	delete(s.bySeg, partner.Seg)
}

// boundHorizontalFarX returns the X of horizontal h's "far" endpoint as
// traversed by bound b. Bound direction (forward = input order, backward =
// reverse input) is inferred from the first non-horizontal segment's
// Reversed flag — non-horizontals in a forward bound are Reversed=false,
// in a backward bound Reversed=true (DESIGN.md §12.10.4).
func boundHorizontalFarX(b *Bound, h *Segment) fixed.Coord {
	forward := true
	for _, seg := range b.Segs {
		if !seg.Horizontal() {
			forward = !seg.Reversed
			break
		}
	}
	// far is canonical Top iff traversal +X (forward AND h not reversed,
	// or backward AND h reversed).
	if forward == !h.Reversed {
		return h.Top.X
	}
	return h.Bot.X
}

// handleHorizMin handles a local-min horizontal: spawn a new ring whose two
// ascending bounds are the AEL entries at the horizontal's endpoints. See
// DESIGN.md §11.8 / §12.6.
//
// Sequencing: the horizontal's event Y is the horizontal's own Y, and the
// EventHoriz fires at h.Top so that both endpoint Bot events have already
// inserted their AEL entries.
//
// AddLocalMinPoly is called with (rightAE, leftAE) — i.e. FrontEdge=rightAE
// and BackEdge=leftAE. This matches the de facto orientation produced by
// [sweep.handleLocalMinimum] for non-horizontal local minima (set by the
// heap order of the two Bot events) and gives the resulting OutPt cycle a
// CCW Next-direction for CCW input. The DESIGN.md §12.3 wording about
// "front=leftmost" is inverted in our code; see [DESIGN.md §12.3] for the
// formal statement and TODO to reconcile.
func (s *sweep) handleHorizMin(e Event) {
	h := e.SegA
	info := s.horiz[h]
	if info == nil {
		return
	}
	leftAE := s.bySeg[info.LeftAdj]
	rightAE := s.bySeg[info.RightAdj]
	if leftAE == nil || rightAE == nil {
		return
	}
	if !leftAE.Contributing || !rightAE.Contributing {
		return
	}
	AddLocalMinPoly(s.ael, rightAE, leftAE, h.Bot, true)
	AddOutPt(rightAE, h.Top)
}

// handleHorizMax handles a local-max horizontal: close the ring whose two
// ascending bounds reach their top at the horizontal's endpoints. See
// DESIGN.md §11.8 / §12.6.
//
// Sequencing: EventHorizMaxOpen fires before the two adjacent Top events at
// the same Y, so the verticals are still in the AEL when this runs.
// handleHorizMax removes the verticals and marks them as already-closed so
// the subsequent EventTop events become no-ops (their seg is gone from
// s.bySeg).
func (s *sweep) handleHorizMax(e Event) {
	h := e.SegA
	info := s.horiz[h]
	if info == nil {
		return
	}
	leftAE := s.bySeg[info.LeftAdj]
	rightAE := s.bySeg[info.RightAdj]
	if leftAE == nil || rightAE == nil {
		return
	}
	if leftAE.Contributing && rightAE.Contributing && leftAE.IsHotEdge() && rightAE.IsHotEdge() {
		AddOutPt(rightAE, h.Top)
		AddLocalMaxPoly(s.ael, leftAE, rightAE, h.Bot)
	}
	s.ael.Remove(leftAE)
	s.ael.Remove(rightAE)
	delete(s.bySeg, info.LeftAdj)
	delete(s.bySeg, info.RightAdj)
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
