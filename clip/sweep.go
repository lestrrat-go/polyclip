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

	// horzSegList accumulates trial horizontal-join anchors emitted during the
	// current scanline's horizontal pass; horzJoinList accumulates the
	// confirmed joins built from them at end-of-scanline. See [horzjoin.go] /
	// Clipper2 horz_seg_list_ / horz_join_list_ (DESIGN.md §12.11).
	horzSegList  []*horzSegment
	horzJoinList []horzJoin

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
			s.handleScanlineBound(evs, y)
		} else {
			s.handleScanlineLegacy(evs)
		}
		// Horizontal pass: every bound whose cursor reached a horizontal at
		// this Y is processed now, with the AEL fully settled by the
		// Top/LocalMin events above. doHorizontal may promote a cursor onto a
		// further horizontal at the same Y; it appends to pendingHoriz, so the
		// loop drains until none remain. Per DESIGN.md §12.6 / §12.10.
		s.flushPendingHoriz(y)
		// A bound that reached a shared vertex via a horizontal (its far
		// endpoint coincides with another source's local-min/through vertex)
		// is only settled into the AEL after the horizontal flush. doHorizontal
		// deliberately does not cross edges sitting exactly at its far endpoint,
		// so reconcile again now that every cursor at y has settled.
		if s.boundModel {
			s.reconcileSharedVertexCrossings(y)
		}
		// End of this scanline's horizontal processing: pair overlapping
		// opposite-direction horizontal runs into deferred joins, then clear
		// the trial list (Clipper2 ExecuteInternal, engine.cpp:2132). Xor does
		// not use the horz-join pass (its coincident horizontals are resolved by
		// the standard maximum handling).
		if s.op != OpXor && len(s.horzSegList) > 0 {
			s.convertHorzSegsToJoins()
		}
		prevY = y
		started = true
	}
	// Splice every deferred horizontal join now that the global ring topology
	// is settled (Clipper2 ExecuteInternal's final ProcessHorzJoins, eng:2143).
	if s.op != OpXor {
		s.processHorzJoins()
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
func (s *sweep) handleScanlineBound(evs []Event, y fixed.Coord) {
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
	// Process the max/intermediate horizontals reached by this scanline's tops
	// (queued by advanceBoundCursor / closeBound) BEFORE classifying this
	// scanline's local minima. A local-max horizontal plateau must leave the
	// AEL before a coincident other-source local min is classified, or the
	// min's WindOther left-walk counts the closing plateau as if its source
	// still continued above — misclassifying the min as non-contributing and
	// dropping its ring (the shared-collinear-horizontal bug, DESIGN.md §12.11).
	// This mirrors Clipper2's phasing: DoTopOfScanbeam + DoHorizontal at a
	// scanline run before the NEXT iteration's InsertLocalMinimaIntoAEL at the
	// same Y (engine.cpp:2127). Min (leading) horizontals are queued later, in
	// the localMins loop below, and flushed by [run] after this returns.
	s.flushPendingHoriz(y)
	// Shared-vertex crossings: after every cursor has advanced through this
	// scanline's vertices, two bounds that pass through the SAME vertex may
	// have swapped left-right order there (one was left below the vertex and
	// is right above it). doIntersections cannot see this — at the vertex the
	// two below-segments meet as a Touch on the beam boundary, not a
	// ProperCross strictly inside the open beam — so the crossing is dispatched
	// here instead, mirroring handleLocalMin's bubble (DESIGN.md §12.11).
	s.reconcileSharedVertexCrossings(y)
	for _, e := range localMins {
		s.handleLocalMin(e)
		s.appendTrace(e, nil)
	}
	for _, e := range intersects {
		s.handleIntersection(e)
		s.appendTrace(e, nil)
	}
}

// reconcileSharedVertexCrossings dispatches crossings that occur exactly at a
// shared vertex on scanline y. After SplitTJunctions every vertex-on-edge is a
// clean shared vertex, so two bounds (from either source) can pass through the
// same point and exchange left-right order there. Such a crossing is invisible
// to doIntersections: at the vertex the two lower segments meet as a Touch on
// the beam boundary rather than a ProperCross strictly inside the open beam, so
// IntersectEdges is never called and hot/cold status (and winding) fail to flip
// — the symptom the d50048a AddLocalMaxPoly workaround patched (DESIGN.md
// §12.11, "shared-vertex crossing dispatch").
//
// All edges sharing a vertex V at this scanline have CurrX == V.X (an edge with
// V strictly interior would have been split by SplitTJunctions). So a pair of
// AEL-adjacent edges with equal CurrX that is now out of slope order has
// crossed at that shared vertex; IntersectEdges processes the §12.5 transition
// and swaps them. This is the through-vertex analog of handleLocalMin's bubble
// (sweep.go local-min IsValidAelOrder loop). The outer loop repeats until no
// adjacent inversion remains, resolving multi-bound confluences at one vertex.
func (s *sweep) reconcileSharedVertexCrossings(y fixed.Coord) {
	for {
		swapped := false
		for i := 0; i+1 < s.ael.Len(); i++ {
			l := s.ael.At(i)
			r := s.ael.At(i + 1)
			if l.CurrX != r.CurrX || !s.ael.Less(r, l) {
				continue
			}
			// Only a genuine through-vertex crossing AT this scanline belongs to
			// reconcile. When the two edges instead properly cross STRICTLY ABOVE
			// y, their CurrX merely coincide here by grid rounding (the crossing
			// rounds to y+ε just past the beam top, so doIntersections deferred it
			// to the next beam, where it will be applied). Crossing it here too
			// would double-dispatch it — once as a phantom local-min opening a
			// zero-area ring, then again as the real crossing — dropping a whole
			// region (holed/non-convex large-coord inputs). Leave it to the next
			// beam's doIntersections.
			if res := Intersect(*l.Seg, *r.Seg); res.Kind == ProperCross && res.P.Y > y {
				continue
			}
			IntersectEdges(s.ael, s.op, l, r, fixed.Point{X: l.CurrX, Y: y})
			swapped = true
		}
		if !swapped {
			return
		}
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
	s.handoffMaxThroughVertex(ae, maxPt)
	// Case C (simultaneous maxima): the partner bound is adjacent in the AEL
	// and reaches maxPt at the same scanline event. AddLocalMaxPoly closes the
	// ring (same OutRec) or joins two rings (different OutRecs — e.g. the
	// central peak of a W-shape). FRONT edge passed first by convention so the
	// local-max vertex prepends to Pts. Gated on IsHotEdge (not Contributing):
	// a post-swap reclassification can leave an edge non-contributing yet still
	// hot, and its ring must still close/join (DESIGN.md §12.10.7 Rule 1).
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

	// Plateau maximum whose other bound reaches maxPt via a horizontal that has
	// not yet been traversed: defer. Clipper2 treats ae's vertex as INTERMEDIATE
	// (the maxima vertex is the horizontal's far end) and closes the pair only
	// when DoHorizontal reaches ae. Running the Case A deferred handoff here
	// instead removes ae before the horizontal arrives; an intervening
	// cross-source crossing on the horizontal then SwapOutrecs the coupled bound
	// into another ring, so the pair never closes cleanly and the rings tangle
	// (the hot-through shared-apex confluence, DESIGN.md §12.11). Leaving ae hot
	// lets the horizontal's own closeBound pair with it via maximaPartner, and
	// resolveBetweenMaxima crosses any between-edges (e.g. the other source's
	// through-bound) at the apex first.
	if s.plateauPartnerPending(ae, maxPt) || s.plateauMaxPartnerPending(ae, maxPt) {
		return
	}

	// Cross-source ring self-closure at a through-vertex. ae's bound maxes out
	// at maxPt, and its OWN ring's other edge (coupled) is a DIFFERENT-source hot
	// edge that passes THROUGH maxPt and continues strictly above (a vertex of
	// ae's source lies on the through-edge, or vice versa — SplitTJunctions made
	// it a shared vertex). The two edges are the two open ends of the same ring
	// meeting at maxPt.
	//
	// Whether the ring CLOSES here depends on the op-region ABOVE maxPt, which is
	// not visible from the local winding (identical in the close and keep cases).
	// ae's source departs at maxPt; reclassify coupled against the AEL with ae
	// removed to see its winding above. If coupled is then NON-contributing the
	// op-region ends at maxPt, so the ring must close and coupled continues as a
	// COLD edge. Without this close, coupled keeps ae's ring open and its upward
	// continuation drags a spurious sub-loop in, tangling the ring so the true
	// region cancels (DESIGN.md §12.11, vertex-on-edge Intersect tangle). If
	// coupled stays contributing above (e.g. an Xor A-only region continues
	// upward), the region does NOT end here — fall through to the normal handoff
	// so coupled keeps building.
	if coupled := outrecOther(ae); coupled != nil && coupled.IsHotEdge() &&
		coupled.Outrec == ae.Outrec && coupled.Seg.Src != ae.Seg.Src &&
		boundContinuesAbove(coupled, maxPt) && throughVertexOnColumn(coupled.Seg, maxPt) {
		savedWS, savedWO, savedC := coupled.WindSelf, coupled.WindOther, coupled.Contributing
		iAe := s.ael.IndexOf(ae)
		left, right := s.maximaFlanks(ae)
		s.ael.Remove(ae)
		Classify(s.ael, coupled, s.op)
		contribAbove := coupled.Contributing
		coupled.WindSelf, coupled.WindOther, coupled.Contributing = savedWS, savedWO, savedC
		if !contribAbove {
			AddLocalMaxPoly(s.ael, ae, coupled, maxPt)
			delete(s.bySeg, ae.Seg)
			if left != nil && right != nil && left != coupled && right != coupled {
				s.maybeScheduleIntersect(left, right, maxPt.Y)
			}
			return
		}
		s.ael.InsertAt(iAe, ae)
	}

	// Hole-notch apex reconnection. ae is a HOT bound reaching its apex maxPt with
	// no same-source maxima partner (the partner already left) and a coupled other
	// edge still building. A COLD cross-source neighbour c continues strictly above
	// maxPt and rejoins maxPt through its just-traversed horizontal (c's bound's
	// previous segment is the horizontal from maxPt to c's current Bot). This is
	// the Intersect hole-notch exit: the intersection boundary rode ae (a subject
	// hole bound) up to the hole apex and must now turn onto the clip bound c that
	// re-bounds the region above. Plain Case A would drop ae and leave c cold,
	// collapsing the ring to a sliver (DESIGN.md §12.11). Instead emit maxPt and the
	// horizontal's far end (c's Bot), then transfer ae's ring side to c so it keeps
	// building; the ring closes when c meets the coupled edge at their shared apex.
	if c := s.apexNotchContinuation(ae, maxPt); c != nil {
		AddOutPt(ae, maxPt)
		AddOutPt(ae, c.Seg.Bot)
		or := ae.Outrec
		if or.FrontEdge == ae {
			or.FrontEdge = c
		} else {
			or.BackEdge = c
		}
		c.Outrec = or
		ae.Outrec = nil
		s.ael.Remove(ae)
		delete(s.bySeg, ae.Seg)
		return
	}

	// No simultaneous partner. The two bounds of this maximum arrive at
	// different events — e.g. a flat top (local-max plateau) whose two
	// ascending bounds reach the plateau ends as separate Top/horizontal
	// events. Use the OutRec coupling (which persists after AEL removal) to
	// hand off between them (DESIGN.md §12.10.5 Cases A/B).
	coupled := outrecOther(ae)

	// Case B: the coupled partner already ran Case A (it emitted maxPt and was
	// removed from the AEL but left the coupling intact). Close the ring,
	// emitting this edge's own apex first when it is a genuinely new vertex —
	// distinct from both ends of the open chain (head and back OutPts).
	//
	// For a same-source plateau ae and the partner top out at the SAME apex, so
	// Case A's vertex already sits at a ring end and emitting again would leave a
	// zero-length edge — the guard skips it. But for a cross-source ring whose
	// two hot edges terminate at DIFFERENT points joined by a horizontal top (a
	// partial collinear-horizontal overlap that SplitOverlaps resolved into a
	// coincident pair — e.g. one source's max edge ends left of the other's),
	// this apex differs from both ends and must be emitted, or the ring
	// collapses to a degenerate two-point sliver (DESIGN.md §12.11).
	if coupled != nil && s.ael.IndexOf(coupled) < 0 {
		if outrec := ae.Outrec; outrec != nil && outrec.Pts != nil {
			head := outrec.Pts
			tail := head.Next
			if maxPt != head.P && maxPt != tail.P {
				AddOutPt(ae, maxPt)
			}
		}
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

// handoffMaxThroughVertex resolves the degenerate crossing where a HOT maximum
// edge ae ends at a shared vertex maxPt that another source's bound passes
// THROUGH (a through-vertex on the apex column). Below maxPt ae is the union
// boundary; at maxPt ae terminates and the other bound's continuing edge takes
// over the boundary. doIntersections cannot see this — ae ends at maxPt, so the
// pair meet as a Touch on the beam boundary, not a ProperCross strictly inside —
// and reconcileSharedVertexCrossings cannot either, because ae does not continue
// above maxPt and so produces no AEL inversion. So dispatch the crossing here,
// before ae's maximum closes, handing ae's hot ring onto the through edge via
// IntersectEdges (DESIGN.md §12.11, overlapping shared-vertex mis-merge).
//
// Only an AEL-adjacent edge that (a) passes through maxPt (on the apex column),
// (b) is NOT ae's maxima partner, and (c) CONTINUES strictly above maxPt — its
// bound's ultimate apex is higher than maxPt — qualifies. An edge whose bound
// tops out at maxPt is a genuine maximum handled by the partner / confluence
// logic, not a through-vertex. Condition (c) is read from the bound's apex
// rather than the cursor's current segment, because the cursor's position at a
// confluence is timing-dependent (see [boundContinuesAbove]).
func (s *sweep) handoffMaxThroughVertex(ae *ActiveEdge, maxPt fixed.Point) {
	// A genuine same-source maximum partner at maxPt means ae's source CLOSES a
	// maximum here (e.g. a concave-notch tip, both A-edges ending at maxPt) — the
	// boundary does not hand off to another bound, it turns around. The
	// maximaPartner / resolveBetweenMaxima path in closeBound handles such a
	// closure, crossing any genuine between-edges itself. Handing off first would
	// SwapOutrecs ae's hot ring onto a through-edge that merely PASSES THROUGH the
	// apex (e.g. the other source's horizontal edge crossing the notch tip),
	// leaving ae cold so the maximum never closes and the rings tangle (DESIGN.md
	// §12.11, notch-tip on a crossing horizontal). The handoff is only for a true
	// vertex-on-edge EXIT where ae's source has no second edge ending at maxPt.
	if s.maximaPartner(ae, maxPt) != nil {
		return
	}
	// crossed guards against re-crossing the same edge: IntersectEdges swaps the
	// pair's AEL positions, so a still-hot ae could otherwise oscillate across
	// the same neighbour. Each through-edge is crossed at most once.
	crossed := map[*ActiveEdge]struct{}{}
	for ae.IsHotEdge() {
		i := s.ael.IndexOf(ae)
		if i < 0 {
			return
		}
		var cand *ActiveEdge
		for _, c := range []*ActiveEdge{s.ael.LeftOf(i), s.ael.RightOf(i)} {
			// The candidate must CONTINUE strictly above maxPt — its bound's
			// ultimate apex is higher than maxPt — so it passes THROUGH maxPt
			// rather than terminating there. An edge whose bound tops out at
			// maxPt is a genuine maximum (a same-source plateau partner or a
			// cross-source co-maximum), handled by the partner / confluence logic.
			//
			// This is decided from the bound's apex, NOT from the cursor's current
			// segment, because the cursor's position at maxPt is timing-dependent:
			// the through-bound may still sit on the segment ENDING at maxPt (Top
			// == maxPt) or may have already advanced onto the segment LEAVING it
			// (Bot == maxPt). Both are the same through-vertex. (IsBoundLast and a
			// current-segment Top test each get one of the two timings wrong.)
			if c == nil || !boundContinuesAbove(c, maxPt) || isMaximaPartner(ae, c, maxPt) {
				continue
			}
			// Only a COLD through-edge qualifies. A cold edge is interior to ae's
			// region just below the vertex and becomes boundary above it (it exits
			// at the shared vertex), so ae's hot ring must hand off onto it. A HOT
			// through-edge already carries its own ring on both sides of the
			// vertex; crossing it here would double-handle and tangle the rings
			// (it surfaces as a same-side AddLocalMaxPoly). Such a confluence is
			// resolved by the maxima / between-maxima logic instead.
			if c.IsHotEdge() {
				continue
			}
			if _, done := crossed[c]; done {
				continue
			}
			if !throughVertexOnColumn(c.Seg, maxPt) {
				continue
			}
			cand = c
			break
		}
		if cand == nil {
			return
		}
		crossed[cand] = struct{}{}
		IntersectEdges(s.ael, s.op, ae, cand, maxPt)
	}
}

// plateauPartnerPending reports whether ae's local maximum is a plateau whose
// OTHER bound reaches maxPt via a horizontal that is still queued in
// pendingHoriz (not yet traversed). ae's coupled OutRec partner is that bound;
// if it currently sits on a horizontal whose far end is maxPt, the maximum is
// closed later by the horizontal pass, so closeBound must not remove ae now.
func (s *sweep) plateauPartnerPending(ae *ActiveEdge, maxPt fixed.Point) bool {
	if !ae.IsHotEdge() {
		return false
	}
	coupled := outrecOther(ae)
	if coupled == nil {
		return false
	}
	for _, h := range s.pendingHoriz {
		if h != coupled {
			continue
		}
		if h.Bound == nil || !h.Seg.Horizontal() || h.Seg.Bot.Y != maxPt.Y {
			return false
		}
		// Defer only when the coupled horizontal genuinely tops out at maxPt — a
		// real shared-plateau maximum the horizontal's own closeBound will close.
		// If that partner CONTINUES above maxPt (it is a mid-bound horizontal of
		// another source that merely passes through the apex), its doHorizontal
		// promotes the cursor past maxPt and never closes ae, stranding ae hot in
		// the AEL where it blocks a higher maximum's partner pairing (DESIGN.md
		// §12.11, coincident max-plateau over a continuing horizontal).
		if boundContinuesAbove(h, maxPt) {
			return false
		}
		// The coupled partner's current plateau segment must end exactly at maxPt
		// (otherwise it is nowhere near closing here).
		if boundHorizontalFarX(h.Bound, h.Seg) != maxPt.X {
			return false
		}
		// When maxPt is the partner's terminal apex it tops out exactly here and
		// its own closeBound closes the shared ring — defer.
		if apex, ok := boundApex(h.Bound); ok && apex == maxPt {
			return true
		}
		// Otherwise the partner's plateau merely PASSES THROUGH maxPt and continues
		// to an apex further along (e.g. a hole's top split into T-junction
		// fragments). Deferring is only safe when the partner borders the OTHER
		// source here (WindOther != 0): the shared cross-source ring is genuine and
		// the partner's eventual close carries ae's ring with it. When the partner
		// is a pure single-source boundary (WindOther == 0) the coupling to ae is
		// incidental — the partner closes its own same-source ring at its apex and
		// ae is orphaned, stranding it hot in the AEL where a later horizontal
		// crosses it and drops a region (the holed-input coincident-plateau bug).
		return h.WindOther != 0
	}
	return false
}

// plateauMaxPartnerPending reports whether ae's GEOMETRIC maxima partner — a
// same-source bound whose apex is also maxPt — will reach maxPt only after
// traversing a trailing horizontal plateau not yet swept this scanline. Unlike
// [plateauPartnerPending], which inspects ae's RING-coupled edge, this looks for
// the partner by geometry, so it fires even when ae is hot on an UNRELATED ring.
//
// That is the holed-input flat-hole-top case (DESIGN.md §12.11): a subject hole
// pokes out of the clip, so the difference region's ring rides the hole's left
// bound up to the hole apex maxPt. The hole's right bound ends in the horizontal
// top whose far end is maxPt, but at the Tops phase — when ae (the left bound)
// tops out and closeBound runs — that horizontal has not been promoted yet, so
// [maximaPartner] misses it and ae closes prematurely (Case A). When the plateau
// also crosses the clip edge, the Case A handoff never reconnects and the rings
// fragment, dropping the region's area. Deferring leaves ae hot in the AEL;
// [doHorizontal]'s own closeBound at the plateau far end then pairs the two via
// maximaPartner and JoinOutrecPaths splices them — the same clean join the
// non-horizontal (tilted-top) variant already produces at the apex.
func (s *sweep) plateauMaxPartnerPending(ae *ActiveEdge, maxPt fixed.Point) bool {
	if !ae.IsHotEdge() || ae.Seg.Horizontal() || ae.Bound == nil {
		return false
	}
	// Only the hole-notch class: ae is hot on a SAME-source region ring (its
	// coupled partner shares ae's source — e.g. region B bounded by the subject
	// square's edge and the subject hole's edges). A CROSS-source ring (clip edge
	// coupled to a subject edge, as at a coincident-plateau confluence) is closed
	// by the cross-source maximum machinery, and deferring it there mis-times the
	// close and drops area (the holed-input coincident-plateau case, DESIGN.md
	// §12.11). Restricting to same-source coupling leaves that path untouched.
	other := outrecOther(ae)
	if other == nil || other.Seg.Src != ae.Seg.Src {
		return false
	}
	for i := range s.ael.Len() {
		cand := s.ael.At(i)
		if cand == ae || cand.Bound == nil || cand.Seg.Src != ae.Seg.Src {
			continue
		}
		last := cand.Bound.Last()
		if last == nil || !last.Horizontal() || last.Bot.Y != maxPt.Y {
			continue
		}
		if apex, ok := boundApex(cand.Bound); !ok || apex != maxPt {
			continue
		}
		// cand reaches maxPt only after traversing its trailing horizontal. If its
		// cursor is already on that horizontal AT maxPt, it is no longer pending —
		// maximaPartner handles it directly.
		if cand.Seg == last && cand.CurrX == maxPt.X {
			continue
		}
		return true
	}
	return false
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
// bound, like ae's, reaches its last segment at maxPt AND belongs to the same
// source. Returns nil if none qualifies. Mirrors Clipper2's GetMaximaPair
// (engine.cpp:254), which pairs by vertex_top POINTER identity — the two
// bounds meeting at the same physical apex of the same input ring.
//
// The same-source requirement is what makes a SHARED-apex confluence correct:
// when two different polygons reach their local maximum at the same coordinate
// (four bounds converging on one point), each polygon's two bounds form a
// maxima pair, NOT the nearest coincident edge of the other source. Pairing by
// bare coordinate coincidence grabbed the wrong (other-source) edge, so the
// hot ring spanning both sources was never closed at the apex (DESIGN.md
// §12.11, "shared local-MAX confluence"). The cross-source edges sitting
// between the pair are crossed by [resolveBetweenMaxima], which transfers the
// hot ring onto the surviving same-source bound that then closes it.
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
		// confluence pair. The base test is XAtY == maxPt.X. ADDITIONALLY accept
		// a between-edge that has turned onto a HORIZONTAL at the apex and whose
		// bound passes THROUGH maxPt continuing strictly above — a genuine
		// through-vertex (another source's bound crossing a concave shared-vertex
		// maximum). XAtY returns a horizontal's Bot.X (the wrong end), so that
		// case fails the base test; throughVertexOnColumn tests the apex lies on
		// the horizontal's span. The extra boundContinuesAbove guard is essential:
		// a horizontal that itself tops out here is part of a coincident
		// max-plateau, NOT a through-edge, and must not widen pairing across it
		// (DESIGN.md §12.11). This clause is purely additive — it never rejects a
		// between-edge the base test already accepted.
		onColumn := XAtY(cand.Seg, maxPt.Y) == maxPt.X ||
			(cand.Seg.Horizontal() && throughVertexOnColumn(cand.Seg, maxPt) &&
				boundContinuesAbove(cand, maxPt))
		if !onColumn {
			return nil
		}
	}
	return nil
}

// isMaximaPartner reports whether cand is a valid maxima partner of ae: a
// distinct bound-last edge reaching maxPt.
func isMaximaPartner(ae, cand *ActiveEdge, maxPt fixed.Point) bool {
	return cand != nil && cand != ae && cand.Bound != nil &&
		cand.IsBoundLast() && boundMaxPt(cand) == maxPt &&
		cand.Seg.Src == ae.Seg.Src
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

// throughVertexOnColumn reports whether seg passes through maxPt on the apex
// column at maxPt.Y. For a sloped segment that is XAtY(seg, maxPt.Y) ==
// maxPt.X. For a horizontal segment whose endpoint is the through-vertex (the
// bound turns onto a horizontal at maxPt before continuing above), XAtY returns
// the segment's Bot.X — the wrong end — so test instead that maxPt lies on the
// horizontal's x-span at maxPt.Y (DESIGN.md §12.11, shared-vertex exit via a
// horizontal).
func throughVertexOnColumn(seg *Segment, maxPt fixed.Point) bool {
	if !seg.Horizontal() {
		return XAtY(seg, maxPt.Y) == maxPt.X
	}
	if seg.Bot.Y != maxPt.Y {
		return false
	}
	lo, hi := seg.Bot.X, seg.Top.X
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo <= maxPt.X && maxPt.X <= hi
}

// boundContinuesAbove reports whether ae's bound tops out strictly above maxPt
// — i.e. ae passes THROUGH maxPt rather than terminating there. It reads the
// bound's final segment (its ultimate apex) so the answer does not depend on
// where the cursor currently sits, which is timing-dependent at a confluence.
func boundContinuesAbove(ae *ActiveEdge, maxPt fixed.Point) bool {
	if ae.Bound == nil {
		return false
	}
	last := ae.Bound.Last()
	if last == nil {
		return false
	}
	return last.Top.Y > maxPt.Y
}

// apexNotchContinuation finds the COLD cross-source neighbour that ae's ring must
// turn onto at the hole-notch apex maxPt, or nil when this is not that case. ae is
// a hot bound reaching its apex with its coupled other edge still building (the
// ring stays open through it). The candidate c is an AEL neighbour of ae that:
//   - is cold and from the other source,
//   - continues strictly above maxPt (boundContinuesAbove), and
//   - rejoins maxPt through its just-traversed horizontal: c sits at maxPt's Y and
//     c's bound's previous segment is the horizontal joining maxPt to c's Bot.
//
// That horizontal was traversed COLD (the clip bound went cold at the notch-entry
// crossing), so c never picked the ring back up; the close must hand ae's ring
// side to c and emit the horizontal so the boundary continues (DESIGN.md §12.11).
func (s *sweep) apexNotchContinuation(ae *ActiveEdge, maxPt fixed.Point) *ActiveEdge {
	if !ae.IsHotEdge() {
		return nil
	}
	coupled := outrecOther(ae)
	if coupled == nil || s.ael.IndexOf(coupled) < 0 {
		return nil
	}
	i := s.ael.IndexOf(ae)
	if i < 0 {
		return nil
	}
	for _, c := range []*ActiveEdge{s.ael.LeftOf(i), s.ael.RightOf(i)} {
		if c == nil || c == coupled || c.IsHotEdge() || c.Bound == nil {
			continue
		}
		if c.Seg.Src == ae.Seg.Src || c.EdgeIdx == 0 {
			continue
		}
		if c.Seg.Bot.Y != maxPt.Y || !boundContinuesAbove(c, maxPt) {
			continue
		}
		prev := c.Bound.Segs[c.EdgeIdx-1]
		if !prev.Horizontal() || prev.Bot.Y != maxPt.Y {
			continue
		}
		// The previous horizontal must join maxPt to c's current Bot.
		if (prev.Bot == maxPt && prev.Top == c.Seg.Bot) || (prev.Top == maxPt && prev.Bot == c.Seg.Bot) {
			return c
		}
	}
	return nil
}

// boundApex returns the terminal (local-maximum) vertex of bound b: the far
// endpoint of its last segment as traversed. For a bound ending in a trailing
// horizontal it is that horizontal's far X at the plateau Y; otherwise the last
// segment's Top. Returns ok=false for an empty bound.
func boundApex(b *Bound) (fixed.Point, bool) {
	last := b.Last()
	if last == nil {
		return fixed.Point{}, false
	}
	if last.Horizontal() {
		return fixed.Point{X: boundHorizontalFarX(b, last), Y: last.Bot.Y}, true
	}
	return last.Top, true
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
		//
		// Register the near endpoint as a trial horizontal-join anchor
		// (Clipper2 DoHorizontal's leading AddTrialHorzJoin, engine.cpp:2567).
		if s.op != OpXor && horz.IsHotEdge() {
			s.addTrialHorzJoin(getLastOp(horz))
		}

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
			// The op IntersectEdges just emitted for the horizontal is a trial
			// join anchor (Clipper2 AddTrialHorzJoin(GetLastOp(horz)),
			// engine.cpp:2657). horz may have gone cold (its ring closed), so guard.
			if s.op != OpXor && horz.IsHotEdge() {
				s.addTrialHorzJoin(getLastOp(horz))
			}
		}

		// Reached the far end. If this horizontal is the bound's last
		// segment, the bound ends at a local max.
		if horz.IsBoundLast() {
			s.closeBound(horz, fixed.Point{X: farX, Y: y})
			return
		}

		// Emit the far endpoint, then promote the cursor in place.
		if horz.IsHotEdge() {
			op := AddOutPt(horz, fixed.Point{X: farX, Y: y})
			// Far endpoint of an intermediate horizontal is a trial join anchor
			// (Clipper2 DoHorizontal's intermediate AddTrialHorzJoin, eng:2691).
			if s.op != OpXor {
				s.addTrialHorzJoin(op)
			}
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
