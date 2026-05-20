package clip

import (
	"sort"

	"github.com/lestrrat-go/polyclip/fixed"
)

// Sweep runs the scanline sweep over segs for the given boolean operation
// and returns both a trace of processed events and the constructed output
// rings. See DESIGN.md §11.5 and §12.5 / §12.6 for the algorithm.
//
// segs is taken by value; callers should not mutate the slice after calling
// Sweep, because the sweep retains pointers into it.
//
// Horizontal-segment support: in the bound model (DESIGN.md §12.6.1)
// horizontals are first-class AEL edges. A bound cursor that lands on a
// horizontal is queued and processed by [sweep.doHorizontal], which walks the
// AEL crossing every edge inside the horizontal's span (one IntersectEdges
// path, no special-case synth-intersect). The legacy per-edge fallback (via
// [ClassifyHorizontals]) runs only when [BuildLocalMinima] fails to
// reconstruct ring topology; that fallback strictly rejects mid-bound
// horizontals and surfaces [ErrUnsupportedHorizontal] via SweepResult.Err.
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

// fallbackTrace is a debug helper: when fallbackTraceEnabled, [newSweep]
// appends the BuildLocalMinima error message every time it falls back to
// the legacy per-edge path. Used by an audit test to enumerate which
// inputs exercise the fallback.
var (
	fallbackTrace        []string
	fallbackTraceEnabled bool
)

// SetFallbackTraceEnabled toggles the BuildLocalMinima-fallback trace.
// For audit tests only.
func SetFallbackTraceEnabled(b bool) { fallbackTraceEnabled = b }

// FallbackTrace returns a copy of the BuildLocalMinima-failure messages
// recorded since the last [ClearFallbackTrace]. For audit tests only.
func FallbackTrace() []string {
	out := make([]string, len(fallbackTrace))
	copy(out, fallbackTrace)
	return out
}

// ClearFallbackTrace resets the fallback trace buffer.
func ClearFallbackTrace() { fallbackTrace = fallbackTrace[:0] }

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

	// pendingHoriz holds bound-model ActiveEdges whose cursor currently sits
	// on a horizontal segment, awaiting the horizontal pass ([doHorizontal]).
	// They are flushed at the end of each scanline Y, AFTER every Top/LocalMin
	// at that Y has settled the AEL (the Top < Bot < Horiz phasing of
	// DESIGN.md §12.6 / §12.10). Cleared by [flushPendingHoriz].
	pendingHoriz []*ActiveEdge

	// boundModel is true when [BuildLocalMinima] succeeded and every segment
	// is claimed by a bound — the scanline is then processed in Clipper2's
	// beam phases (intersections, then ALL tops, then ALL local minima, then
	// horizontals). False for the legacy per-edge fallback, which dispatches
	// per (Y, X) point via [handleBatch].
	boundModel bool
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
	if mErr != nil && fallbackTraceEnabled {
		fallbackTrace = append(fallbackTrace, mErr.Error())
	}
	if mErr == nil {
		s.boundModel = true
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
	started := false
	var prevY fixed.Coord
	for s.queue.Len() > 0 {
		y := s.queue.Peek().P.Y
		// Resolve every edge crossing inside the scanbeam (prevY, y) from the
		// settled AEL BEFORE handling this scanline's events (DESIGN.md §12.11).
		// The AEL is still ordered for prevY here; doIntersections swaps the
		// crossed edges so the order is correct for y's Top/LocalMin handlers.
		// This is Clipper2's DoIntersections model and replaces incremental
		// per-adjacency scheduling, which silently missed crossings whenever an
		// adjacency formed without a fresh pairwise check.
		if s.boundModel && started {
			s.doIntersections(prevY, y)
		}
		// Collect every event at this scanline Y.
		var evs []Event
		for s.queue.Len() > 0 && s.queue.Peek().P.Y == y {
			evs = append(evs, s.queue.Pop())
		}
		if s.boundModel {
			// Refresh every active edge's CurrX to this scanline so that
			// sorted inserts (local-min spawns) and Classify's left-walk see
			// the true left-to-right order at Y. Without this, a new edge
			// spawning at a high-Y local minimum is placed using neighbours'
			// stale CurrX (from their lower-Y events), corrupting the winding
			// classification (DESIGN.md §11.10 invariant 1). Mirrors Clipper2's
			// per-scanbeam curr_x update in DoTopOfScanbeam.
			s.ael.UpdateForScanline(y)
			s.handleScanlineBound(evs)
		} else {
			s.handleScanlineLegacy(evs)
		}
		// Horizontal pass: every bound whose cursor reached a horizontal at
		// this Y is processed now, with the AEL fully settled by the
		// Top/LocalMin events above. doHorizontal may promote a cursor onto a
		// further horizontal at the same Y; it appends to pendingHoriz, so the
		// loop drains until none remain. Per DESIGN.md §12.6 / §12.10.
		s.flushPendingHoriz(y)
		prevY = y
		started = true
	}
}

// doIntersections resolves every edge crossing strictly inside the scanbeam
// (botY, topY) from the current (botY-ordered) AEL, processing them bottom-up.
// Port of Clipper2's DoIntersections / BuildIntersectList / ProcessIntersectList
// (engine.cpp), per DESIGN.md §12.11. Every AEL edge spans the whole beam
// (each vertex Y is an event and topY is the next one), so a pair crosses in
// this beam iff its [Intersect] is a ProperCross with botY < pt.Y < topY.
func (s *sweep) doIntersections(botY, topY fixed.Coord) {
	nodes := s.buildIntersectList(botY, topY)
	if len(nodes) == 0 {
		return
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].pt.Y != nodes[j].pt.Y {
			return nodes[i].pt.Y < nodes[j].pt.Y
		}
		return nodes[i].pt.X < nodes[j].pt.X
	})
	s.processIntersectList(nodes)
}

// intersectNode is one crossing pending in [sweep.doIntersections].
type intersectNode struct {
	a, b *ActiveEdge
	pt   fixed.Point
}

// buildIntersectList enumerates all edge-pair crossings strictly inside
// (botY, topY). O(n²) per beam (correctness-first; a merge-sort inversion
// counter à la Clipper2 BuildIntersectList is the later optimisation).
func (s *sweep) buildIntersectList(botY, topY fixed.Coord) []intersectNode {
	var nodes []intersectNode
	n := s.ael.Len()
	for i := range n {
		ei := s.ael.At(i)
		for j := i + 1; j < n; j++ {
			ej := s.ael.At(j)
			res := Intersect(*ei.Seg, *ej.Seg)
			if res.Kind != ProperCross {
				continue
			}
			// Beam is (botY, topY]: a crossing at botY was resolved in the
			// previous beam (as its topY); one at topY must be applied here,
			// before topY's Top/LocalMin events (Clipper2 clamps boundary
			// crossings into the beam rather than dropping them).
			if res.P.Y <= botY || res.P.Y > topY {
				continue
			}
			nodes = append(nodes, intersectNode{a: ei, b: ej, pt: res.P})
		}
	}
	return nodes
}

// processIntersectList applies the (Y,X-sorted) crossings bottom-up. The
// lowest crossing's edges are AEL-adjacent; if rounding leaves a node's edges
// non-adjacent, advance to the next node whose edges ARE adjacent and process
// it first (Clipper2 ProcessIntersectList's edit). [IntersectEdges] performs
// the swap, reclassification, and output emission.
func (s *sweep) processIntersectList(nodes []intersectNode) {
	for i := range nodes {
		if !s.edgesAdjacent(nodes[i]) {
			k := i + 1
			for k < len(nodes) && !s.edgesAdjacent(nodes[k]) {
				k++
			}
			if k == len(nodes) {
				// No adjacent node found (degenerate); skip to avoid a bad swap.
				continue
			}
			nodes[i], nodes[k] = nodes[k], nodes[i]
		}
		IntersectEdges(s.ael, s.op, nodes[i].a, nodes[i].b, nodes[i].pt)
	}
}

// edgesAdjacent reports whether the node's two edges are currently neighbours
// in the AEL (a precondition for [IntersectEdges]).
func (s *sweep) edgesAdjacent(nd intersectNode) bool {
	ia := s.ael.IndexOf(nd.a)
	ib := s.ael.IndexOf(nd.b)
	return ia >= 0 && ib >= 0 && absInt(ia-ib) == 1
}

// handleScanlineBound processes all events at one scanline in Clipper2's beam
// phase order (DESIGN.md §12.10.1): intersections (crossings resolve before
// tops), then ALL tops (maxima close / intermediate advance), then ALL local
// minima (spawn). Processing every top before any local minimum is essential:
// at a shared horizontal edge (e.g. vertically stacked squares) the upper
// ring's local minimum must classify against an AEL from which the lower
// ring's maxima edges have already been removed, or it is misclassified as
// interior and never created. Horizontals are flushed afterwards by [run].
func (s *sweep) handleScanlineBound(evs []Event) {
	var tops, localMins, intersects []Event
	for _, e := range evs {
		switch e.Kind {
		case EventTop:
			tops = append(tops, e)
		case EventLocalMin:
			localMins = append(localMins, e)
		case EventIntersection:
			intersects = append(intersects, e)
		}
	}
	for _, e := range tops {
		s.handleTop(e)
		s.appendTrace(e, nil)
	}
	for _, e := range localMins {
		s.handleLocalMin(e)
		s.appendTrace(e, nil)
	}
	for _, e := range intersects {
		s.handleIntersection(e)
		s.appendTrace(e, nil)
	}
}

// handleScanlineLegacy processes a scanline in the per-edge fallback path
// (used when [BuildLocalMinima] fails). Events are grouped per (Y, X) point
// and dispatched by [handleBatch], preserving the configuration detection
// (2-tops = local max, 1-top-1-bot = through-vertex) that the fallback relies
// on.
func (s *sweep) handleScanlineLegacy(evs []Event) {
	for i := 0; i < len(evs); {
		j := i + 1
		for j < len(evs) && evs[j].P == evs[i].P {
			j++
		}
		s.handleBatch(evs[i:j])
		i = j
	}
}

// flushPendingHoriz runs [doHorizontal] for every bound-model ActiveEdge whose
// cursor reached a horizontal at scanline y. doHorizontal may append more
// entries (a promoted cursor landing on another horizontal at the same y), so
// the queue is drained to empty.
func (s *sweep) flushPendingHoriz(y fixed.Coord) {
	for len(s.pendingHoriz) > 0 {
		horz := s.pendingHoriz[0]
		s.pendingHoriz = s.pendingHoriz[1:]
		s.doHorizontal(horz, y)
	}
	s.pendingHoriz = nil
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
// topsAreLocalMax reports whether the two Top events in tops correspond
// to a real local-max meeting: both AEs are at their bound's last segment.
// If either AE is mid-bound (advanceBoundCursor still has work to do),
// the shared point is a through-vertex, NOT a local-max — dispatching to
// handleLocalMaximum in that case incorrectly removes both AEs from the
// AEL.
func topsAreLocalMax(s *sweep, tops []Event) bool {
	if len(tops) != 2 {
		return false
	}
	ae1, ok1 := s.bySeg[tops[0].SegA]
	ae2, ok2 := s.bySeg[tops[1].SegA]
	if !ok1 || !ok2 {
		// Unknown — defer to existing behavior (handleLocalMaximum's
		// own fallback handles missing AEs).
		return true
	}
	return ae1.IsBoundLast() && ae2.IsBoundLast()
}

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
		// Two Tops at the same point: real local-max only if BOTH AEs are
		// at their bound's last segment. Otherwise the AEs belong to
		// different rings (or different bounds of the same ring) and the
		// shared point is a "through-vertex" for one or both. Process
		// each individually via handleTop so advanceBoundCursor handles
		// non-terminal Tops correctly (DESIGN.md §11.7 touching-vertex
		// diamonds case).
		if topsAreLocalMax(s, tops) {
			s.handleLocalMaximum(tops[0], tops[1])
		} else {
			for _, e := range tops {
				s.handleTop(e)
				s.appendTrace(e, nil)
			}
		}
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

// boundWindDx returns the ±1 winding contribution of bound b: the
// [signedContribution] of its first non-horizontal segment. Every segment of
// a bound (including its leading/trailing horizontals) shares this value; an
// [ActiveEdge] caches it in WindDx at spawn so [Classify] can treat the
// horizontal as carrying its bound's winding contribution (DESIGN.md
// §12.6.1). Returns 0 for a degenerate all-horizontal bound.
func boundWindDx(b *Bound) int {
	if seg := firstNonHorizontal(b); seg != nil {
		return signedContribution(seg)
	}
	return 0
}

// handleLocalMin spawns the two ascending bounds of a local minimum into
// the AEL. Replaces the per-segment 2-Bot batched [sweep.handleLocalMinimum]
// for inputs that form closed rings (where [BuildLocalMinima] succeeds).
// Per DESIGN.md §12.1 / §12.7.
//
// Sequence: insert both bound entries (each cursor on its first segment, even
// if horizontal), re-classify (fixes stale winding counts from the
// first-inserted edge), call [AddLocalMinPoly] with (right, left) ordering so
// FrontEdge = Right bound (matching the orientation convention), then activate
// each bound — a horizontal first segment is queued for [sweep.doHorizontal];
// a non-horizontal one schedules its EventTop and intersection checks.
func (s *sweep) handleLocalMin(e Event) {
	lm := e.LocalMin
	if lm == nil {
		return
	}
	// Insert the left bound at its sorted position, then the right bound
	// IMMEDIATELY to its right (adjacent, not sorted). This mirrors Clipper2's
	// InsertLeftEdge + InsertRightEdge: the right bound is later bubbled into
	// sorted order, and every edge it passes is an intersection AT the
	// local-min point that must be processed (the vertex-on-edge / coincident
	// case — DESIGN.md §12.11).
	leftAE := s.spawnBoundActive(lm.Left, lm.Vertex)
	rightAE := s.makeBoundActive(lm.Right, lm.Vertex)
	if leftAE == nil || rightAE == nil {
		return
	}
	s.ael.InsertAt(s.ael.IndexOf(leftAE)+1, rightAE)
	Classify(s.ael, leftAE, s.op)
	Classify(s.ael, rightAE, s.op)
	// AddLocalMinPoly creates the new ring only when both bounds are
	// contributing — for ops like Intersect / Difference, the bounds may
	// emerge non-contributing at a local min and become contributing only
	// after a later intersection swaps in the other source. Activation
	// (EventTop scheduling, horizontal registration, intersection checks)
	// MUST happen regardless, or the AEs sit in the AEL with no advance and
	// the sweep stalls. The ring is created with FrontEdge = Right bound
	// (rightAE passed first) per the orientation convention (DESIGN.md §12.3).
	if leftAE.Contributing && rightAE.Contributing {
		AddLocalMinPoly(s.ael, rightAE, leftAE, lm.Vertex, true)
	}
	// Bubble the right bound rightward past any edge it is out of order with
	// (an edge coincident at the local-min point that sorts before it). Each
	// such pass is an intersection at lm.Vertex: IntersectEdges processes the
	// §12.5 winding/ring transition and swaps, leaving the right bound in
	// sorted order. For non-degenerate minima nothing is out of order and the
	// loop is a no-op (mirrors Clipper2 InsertLocalMinimaIntoAEL's
	// IsValidAelOrder bubble).
	for {
		i := s.ael.IndexOf(rightAE)
		nb := s.ael.RightOf(i)
		if nb == nil || !s.ael.Less(nb, rightAE) {
			break
		}
		IntersectEdges(s.ael, s.op, rightAE, nb, lm.Vertex)
	}
	// Activate both bounds. A bound whose first segment is horizontal (an
	// axial polygon's bottom edge is a local-min horizontal) is queued for
	// the horizontal pass instead of scheduling a Top; doHorizontal walks it
	// and promotes the cursor. Per DESIGN.md §12.6.1 (horizontals are
	// first-class AEL edges).
	s.activateBound(leftAE, lm.Vertex.Y)
	s.activateBound(rightAE, lm.Vertex.Y)
}

// makeBoundActive creates the [ActiveEdge] for bound b's first segment and
// registers it in bySeg, WITHOUT inserting it into the AEL (the caller places
// it). See [spawnBoundActive] for the AEL-inserting variant.
func (s *sweep) makeBoundActive(b *Bound, vertex fixed.Point) *ActiveEdge {
	if b == nil || len(b.Segs) == 0 {
		return nil
	}
	seg := b.Segs[0]
	ae := &ActiveEdge{
		Seg:     seg,
		Bound:   b,
		EdgeIdx: 0,
		CurrX:   vertex.X,
		WindDx:  boundWindDx(b),
	}
	s.bySeg[seg] = ae
	return ae
}

// activateBound schedules the future processing of a freshly-positioned bound
// cursor ae at scanline y. If ae sits on a horizontal segment it is appended
// to pendingHoriz for the end-of-scanline horizontal pass; otherwise its
// EventTop is scheduled and intersection checks against its new AEL neighbours
// are queued.
func (s *sweep) activateBound(ae *ActiveEdge, y fixed.Coord) {
	if ae.Seg.Horizontal() {
		s.pendingHoriz = append(s.pendingHoriz, ae)
		return
	}
	s.queue.Push(Event{Kind: EventTop, P: ae.Seg.Top, SegA: ae.Seg})
	i := s.ael.IndexOf(ae)
	if i < 0 {
		return
	}
	if left := s.ael.LeftOf(i); left != nil {
		s.maybeScheduleIntersect(left, ae, y)
	}
	if right := s.ael.RightOf(i); right != nil {
		s.maybeScheduleIntersect(ae, right, y)
	}
}

// spawnBoundActive creates an [ActiveEdge] for bound b emerging from the
// local-min vertex. The cursor sits on the bound's FIRST segment even when
// that segment is horizontal (DESIGN.md §12.6.1, Stage 2): a leading
// horizontal is a first-class AEL member that [doHorizontal] later walks. The
// edge enters the AEL at vertex.X (the near, local-min end of the first
// segment) with a sweeping CurrX. Returns nil if b is empty.
func (s *sweep) spawnBoundActive(b *Bound, vertex fixed.Point) *ActiveEdge {
	if b == nil || len(b.Segs) == 0 {
		return nil
	}
	seg := b.Segs[0]
	ae := &ActiveEdge{
		Seg:     seg,
		Bound:   b,
		EdgeIdx: 0,
		CurrX:   vertex.X,
		WindDx:  boundWindDx(b),
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
	ae := &ActiveEdge{Seg: e.SegA, CurrX: e.SegA.Bot.X, WindDx: signedContribution(e.SegA)}
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

	// Bound-model path (§12.10.4 / §12.10.5). EventTop fires only for
	// non-horizontal segments (horizontals are processed by doHorizontal),
	// so a bound-last edge here is non-horizontal and its local-max vertex
	// is ae.Seg.Top.
	if ae.Bound != nil {
		if ae.IsBoundLast() {
			s.closeBound(ae, ae.Seg.Top)
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

// advanceBoundCursor promotes ae's bound cursor by ONE segment when the
// current (non-horizontal) edge reaches its Top without ending the bound.
// Per DESIGN.md §12.10.4 the update is IN PLACE — the AE keeps its AEL
// position (mirroring Clipper2's UpdateEdgeIntoAEL at engine.cpp:1731).
//
// The local-max vertex (= currentTop) is emitted onto the ring if ae is hot.
// If the promoted segment is horizontal, ae is queued for the horizontal
// pass ([doHorizontal]); otherwise the next EventTop and fresh intersection
// checks are scheduled.
func (s *sweep) advanceBoundCursor(ae *ActiveEdge, currentTop fixed.Point) {
	if ae.IsHotEdge() {
		AddOutPt(ae, currentTop)
	}
	// IN-PLACE update: do NOT remove/reinsert in the AEL. The slope may
	// change but AEL ordering is fixed by the next scanbeam's intersection
	// pass (§12.10.1).
	delete(s.bySeg, ae.Seg)
	ae.EdgeIdx++
	ae.Seg = ae.Bound.Segs[ae.EdgeIdx]
	s.bySeg[ae.Seg] = ae
	if ae.Seg.Horizontal() {
		// The new horizontal joins at currentTop; that endpoint is the near
		// (sweep) end. doHorizontal walks from here to the far end.
		ae.CurrX = currentTop.X
		s.pendingHoriz = append(s.pendingHoriz, ae)
		return
	}
	ae.CurrX = ae.Seg.Bot.X
	s.queue.Push(Event{Kind: EventTop, P: ae.Seg.Top, SegA: ae.Seg})
	// Schedule intersection checks against the new segment's slope: the
	// previous segment's crossings have been processed but the new edge
	// may cross neighbours that the old one didn't.
	i := s.ael.IndexOf(ae)
	if i < 0 {
		return
	}
	if left := s.ael.LeftOf(i); left != nil {
		s.maybeScheduleIntersect(left, ae, currentTop.Y)
	}
	if right := s.ael.RightOf(i); right != nil {
		s.maybeScheduleIntersect(ae, right, currentTop.Y)
	}
}

// closeBound closes (or merges) the ring at a local maximum, where ae's bound
// cursor has reached its last segment. maxPt is the local-max vertex. This is
// the analog of Clipper2's DoMaxima (engine.cpp:2729). Both callers (handleTop
// for a non-horizontal last segment, doHorizontal for a trailing horizontal)
// pass the resolved maxPt.
//
// The maxima partner is the AEL-ADJACENT edge whose bound also ends at maxPt —
// NOT necessarily ae's own OutRec partner. When two bounds from DIFFERENT
// local minima meet at a shared local maximum (e.g. the central peak of a
// W-shape), they belong to different OutRecs that must be JOINED;
// AddLocalMaxPoly handles both the same-ring close and the two-ring join.
func (s *sweep) closeBound(ae *ActiveEdge, maxPt fixed.Point) {
	// Case C (simultaneous maxima): the partner bound is adjacent in the AEL
	// and reaches maxPt at the same scanline event. AddLocalMaxPoly closes the
	// ring (same OutRec) or joins two rings (different OutRecs — e.g. the
	// central peak of a W-shape). FRONT edge passed first by convention so the
	// local-max vertex prepends to Pts. Gated on IsHotEdge (not Contributing):
	// a post-swap reclassification can leave an edge non-contributing yet still
	// hot, and its ring must still close/join (DESIGN.md §12.10.8 Rule 1).
	if partner := s.maximaPartner(ae, maxPt); partner != nil {
		// Resolve any edges lying strictly between ae and its maxima partner
		// (a multi-edge confluence: another shape's bounds pass through maxPt
		// here). Each is a genuine crossing — IntersectEdges dispatches it
		// through the §12.5 table and reclassifies the between-edge, so its
		// hot/contributing status is updated before the pair closes. After the
		// loop ae and partner are AEL-adjacent. Port of Clipper2 DoMaxima's
		// between-maxima loop (engine.cpp:2756, DESIGN.md §12.6.1 follow-up).
		s.resolveBetweenMaxima(ae, partner, maxPt)
		if ae.IsHotEdge() && partner.IsHotEdge() {
			AddLocalMaxPoly(s.ael, ae, partner, maxPt)
		}
		// Capture the edges flanking the removed pair: once both maxima edges
		// leave, the edge to their left and the edge to their right become
		// adjacent and may cross higher up. Schedule that check, or the
		// crossing is silently missed and the AEL order corrupts later
		// classifications (the cause of lost teeth in unions of concave shapes).
		left, right := s.maximaFlanks(ae, partner)
		s.ael.Remove(ae)
		s.ael.Remove(partner)
		delete(s.bySeg, ae.Seg)
		delete(s.bySeg, partner.Seg)
		if left != nil && right != nil {
			s.maybeScheduleIntersect(left, right, maxPt.Y)
		}
		return
	}

	// No simultaneous partner. The two bounds of this maximum arrive at
	// different events — e.g. a flat top (local-max plateau) whose two
	// ascending bounds reach the plateau ends as separate Top/horizontal
	// events. Use the OutRec coupling (which persists after AEL removal) to
	// hand off between them (DESIGN.md §12.10.5 Cases A/B).
	coupled := outrecOther(ae)

	// Case B: the coupled partner already ran Case A (it emitted maxPt and was
	// removed from the AEL but left the coupling intact). Close the ring
	// without re-emitting the vertex.
	if coupled != nil && s.ael.IndexOf(coupled) < 0 {
		if outrec := ae.Outrec; outrec != nil {
			outrec.FrontEdge = nil
			outrec.BackEdge = nil
		}
		ae.Outrec = nil
		coupled.Outrec = nil
		left, right := s.maximaFlanks(ae)
		s.ael.Remove(ae)
		delete(s.bySeg, ae.Seg)
		if left != nil && right != nil {
			s.maybeScheduleIntersect(left, right, maxPt.Y)
		}
		return
	}

	// Case A: emit maxPt and remove ae from the AEL, but LEAVE the OutRec
	// coupling intact so the partner's eventual close (Case B) finds it.
	if ae.IsHotEdge() {
		AddOutPt(ae, maxPt)
	}
	left, right := s.maximaFlanks(ae)
	s.ael.Remove(ae)
	delete(s.bySeg, ae.Seg)
	if left != nil && right != nil {
		s.maybeScheduleIntersect(left, right, maxPt.Y)
	}
}

// maximaFlanks returns the edges immediately outside the span occupied by the
// given edges (one or two adjacent maxima edges): the edge to the left of the
// leftmost and the edge to the right of the rightmost. After the maxima edges
// are removed these two become adjacent, so a fresh crossing check is needed.
func (s *sweep) maximaFlanks(edges ...*ActiveEdge) (left, right *ActiveEdge) {
	lo, hi := -1, -1
	for _, e := range edges {
		i := s.ael.IndexOf(e)
		if i < 0 {
			continue
		}
		if lo < 0 || i < lo {
			lo = i
		}
		if hi < 0 || i > hi {
			hi = i
		}
	}
	if lo < 0 {
		return nil, nil
	}
	return s.ael.LeftOf(lo), s.ael.RightOf(hi)
}

// outrecOther returns the other active edge (FrontEdge/BackEdge) coupled to
// ae's OutRec, or nil if ae is not hot or has no coupled partner.
func outrecOther(ae *ActiveEdge) *ActiveEdge {
	if ae.Outrec == nil {
		return nil
	}
	if ae.Outrec.FrontEdge != nil && ae.Outrec.FrontEdge != ae {
		return ae.Outrec.FrontEdge
	}
	if ae.Outrec.BackEdge != nil && ae.Outrec.BackEdge != ae {
		return ae.Outrec.BackEdge
	}
	return nil
}

// maximaPartner returns ae's local-maximum partner: the nearest AEL edge whose
// bound, like ae's, reaches its last segment at maxPt. Returns nil if none
// qualifies. Mirrors Clipper2's GetMaximaPair (engine.cpp:254), whose
// vertex_top identity test we approximate with maxPt coincidence (valid for
// non-self-intersecting input, where no two distinct maxima share a point).
//
// The partner need NOT be AEL-adjacent: at a multi-edge confluence another
// ring's bounds may sit between the pair (the interleaved a-L,b-L,a-R,b-R
// case where a's max and b's max differ). The search walks outward
// symmetrically so it finds the partner on whichever side it landed,
// regardless of which maxima edge triggered the close first.
func (s *sweep) maximaPartner(ae *ActiveEdge, maxPt fixed.Point) *ActiveEdge {
	i := s.ael.IndexOf(ae)
	if i < 0 {
		return nil
	}
	// Scan left first then right, preserving the original immediate-neighbour
	// preference (left-first) when both sides qualify.
	if p := s.scanMaximaPartner(ae, i, -1, maxPt); p != nil {
		return p
	}
	return s.scanMaximaPartner(ae, i, +1, maxPt)
}

// scanMaximaPartner walks the AEL from index i in direction dir (+1 right,
// -1 left) looking for ae's maxima partner. It accepts a non-adjacent partner
// only across intermediate edges that all pass through the apex column
// (X == maxPt.X at this scanline): a genuine confluence squeezes every
// between-edge onto maxPt.X, whereas an unrelated edge that merely shares
// maxPt with ae is reached across an off-column edge and is rejected. This is
// what keeps [resolveBetweenMaxima]'s "all between-edges meet at maxPt"
// invariant true.
func (s *sweep) scanMaximaPartner(ae *ActiveEdge, i, dir int, maxPt fixed.Point) *ActiveEdge {
	n := s.ael.Len()
	for k := i + dir; k >= 0 && k < n; k += dir {
		cand := s.ael.At(k)
		if isMaximaPartner(ae, cand, maxPt) {
			return cand
		}
		// cand is an intermediate edge; only continue past it if it lies on
		// the apex column. Otherwise ae and any farther candidate are not a
		// confluence pair.
		if XAtY(cand.Seg, maxPt.Y) != maxPt.X {
			return nil
		}
	}
	return nil
}

// isMaximaPartner reports whether cand is a valid maxima partner of ae: a
// distinct bound-last edge reaching maxPt.
func isMaximaPartner(ae, cand *ActiveEdge, maxPt fixed.Point) bool {
	return cand != nil && cand != ae && cand.Bound != nil &&
		cand.IsBoundLast() && boundMaxPt(cand) == maxPt
}

// resolveBetweenMaxima crosses every edge lying strictly between ae and its
// maxima partner, bubbling ae through them until the two are AEL-adjacent.
// Each between-edge passes through maxPt at this scanline (it is squeezed
// between two bounds converging on maxPt), so IntersectEdges is dispatched at
// maxPt; it swaps ae past the between-edge and reclassifies both. See
// closeBound for the rationale. Port of Clipper2 DoMaxima's between loop
// (engine.cpp:2756-2766).
func (s *sweep) resolveBetweenMaxima(ae, partner *ActiveEdge, maxPt fixed.Point) {
	for {
		i := s.ael.IndexOf(ae)
		j := s.ael.IndexOf(partner)
		if i < 0 || j < 0 || absInt(i-j) <= 1 {
			return
		}
		var between *ActiveEdge
		if i < j {
			between = s.ael.RightOf(i)
		} else {
			between = s.ael.LeftOf(i)
		}
		if between == nil {
			return
		}
		IntersectEdges(s.ael, s.op, ae, between, maxPt)
	}
}

// boundMaxPt returns the local-maximum vertex of ae's bound, assuming ae's
// cursor is on the bound's last segment. For a trailing horizontal it is the
// horizontal's far endpoint; otherwise the segment's Top.
func boundMaxPt(ae *ActiveEdge) fixed.Point {
	if ae.Seg.Horizontal() {
		return fixed.Point{X: boundHorizontalFarX(ae.Bound, ae.Seg), Y: ae.Seg.Bot.Y}
	}
	return ae.Seg.Top
}

// doHorizontal processes a bound whose cursor sits on a horizontal segment at
// scanline y. It is a port of Clipper2's DoHorizontal (engine.cpp:2526) into
// the bound-cursor model (DESIGN.md §12.6 / §12.6.1).
//
// The horizontal sweeps from its near end (horz.CurrX, where the bound
// arrived) to its far end (the bound's continuation vertex). Every AEL edge
// strictly inside that X-span is crossed: [IntersectEdges] dispatches the
// crossing through the §12.5 table and swaps the two edges, so after each
// crossing horz has advanced one position in the walk direction. On reaching
// the far end the cursor is promoted to the bound's next segment (in place,
// like UpdateEdgeIntoAEL); if the horizontal is the bound's last segment the
// ring closes via [closeBound].
func (s *sweep) doHorizontal(horz *ActiveEdge, y fixed.Coord) {
	for {
		nearX := horz.CurrX
		farX := boundHorizontalFarX(horz.Bound, horz.Seg)
		leftToRight := farX >= nearX

		// The near endpoint is already on the ring — emitted as the first
		// OutPt by AddLocalMinPoly (leading horizontal) or by advanceBoundCursor
		// at the vertex it promoted through (mid/trailing horizontal). Emitting
		// it again here can duplicate the vertex when an intervening ring-join
		// (a shared local maximum) has moved the chain head, so doHorizontal
		// only emits crossings and the far endpoint.

		// Walk and intersect every edge strictly inside the span.
		for {
			i := s.ael.IndexOf(horz)
			if i < 0 {
				break
			}
			var e *ActiveEdge
			if leftToRight {
				e = s.ael.RightOf(i)
			} else {
				e = s.ael.LeftOf(i)
			}
			if e == nil {
				break
			}
			eX := XAtY(e.Seg, y)
			if leftToRight && eX > farX {
				break
			}
			if !leftToRight && eX < farX {
				break
			}
			// An edge exactly at the far endpoint is the bound's own
			// continuation vertex or another bound touching there — handled
			// as a local min/max/through-vertex elsewhere, not crossed here.
			if eX == farX {
				break
			}
			pt := fixed.Point{X: eX, Y: y}
			IntersectEdges(s.ael, s.op, horz, e, pt)
			horz.CurrX = eX
		}

		// Reached the far end. If this horizontal is the bound's last
		// segment, the bound ends at a local max.
		if horz.IsBoundLast() {
			s.closeBound(horz, fixed.Point{X: farX, Y: y})
			return
		}

		// Emit the far endpoint, then promote the cursor in place.
		if horz.IsHotEdge() {
			AddOutPt(horz, fixed.Point{X: farX, Y: y})
		}
		delete(s.bySeg, horz.Seg)
		horz.EdgeIdx++
		horz.Seg = horz.Bound.Segs[horz.EdgeIdx]
		horz.CurrX = farX
		s.bySeg[horz.Seg] = horz

		// Consecutive horizontal at the same scanline: keep walking. (Rare;
		// preprocess normally leaves at most one horizontal per scanline per
		// bound, but loop defensively to mirror Clipper2.)
		if horz.Seg.Horizontal() && horz.Seg.Bot.Y == y {
			continue
		}

		// Promoted onto a non-horizontal: schedule its Top and fresh
		// intersection checks, then return.
		s.queue.Push(Event{Kind: EventTop, P: horz.Seg.Top, SegA: horz.Seg})
		if i := s.ael.IndexOf(horz); i >= 0 {
			if left := s.ael.LeftOf(i); left != nil {
				s.maybeScheduleIntersect(left, horz, y)
			}
			if right := s.ael.RightOf(i); right != nil {
				s.maybeScheduleIntersect(horz, right, y)
			}
		}
		return
	}
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
// and BackEdge=leftAE. This is polyclip's caller-side inversion of
// Clipper2's "front=leftmost" convention (DESIGN.md §12.3 convention note);
// it gives the resulting OutPt cycle a CCW Next-direction for CCW input.
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

// handleIntersection processes a queued EventIntersection. Only the legacy
// per-edge fallback path enqueues these now; the bound model resolves crossings
// per scanbeam via [sweep.doIntersections] (DESIGN.md §12.11).
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

// maybeScheduleIntersect enqueues an EventIntersection for a crossing of two
// adjacent edges above currY. It is the legacy fallback's incremental
// scheduler; the bound model uses per-scanbeam [sweep.doIntersections] instead
// and this is a no-op there (DESIGN.md §12.11).
func (s *sweep) maybeScheduleIntersect(left, right *ActiveEdge, currY fixed.Coord) {
	if s.boundModel {
		return
	}
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
