package clip

import "github.com/lestrrat-go/polyclip/fixed"

// IntersectEdges applies DESIGN.md §12.5's decision tree at an intersection
// of two adjacent AEL edges. It performs the AEL swap, updates the two edges'
// winding counts in place (Clipper2's incremental wind_cnt/wind_cnt2 model,
// engine.cpp:1871), then emits output via [AddLocalMinPoly], [AddLocalMaxPoly],
// [AddOutPt], or [SwapOutrecs] as the post-update winding state dictates.
//
// The winding counts are NOT recomputed by a left-walk after the swap (the
// old approach, which let front/back polarity drift — DESIGN.md §12.11). Only
// e1 and e2 change winding at a crossing: every other edge's left-neighbourhood
// multiset is unchanged, so their counts are already correct.
//
// Returns the [OutPt] emitted at pt, or nil when no output was emitted (e.g.
// the intersection is interior to one of the inputs and Union absorbs it).
//
// e1 and e2 may be passed in either order; IntersectEdges canonicalises them
// to (lower AEL index, higher AEL index) before evaluating the tree. If the
// edges are not actually adjacent at the moment of call (a stale event),
// IntersectEdges returns nil without effect.
func IntersectEdges(ael *AEL, op Operation, e1, e2 *ActiveEdge, pt fixed.Point) *OutPt {
	i1 := ael.IndexOf(e1)
	i2 := ael.IndexOf(e2)
	if i1 < 0 || i2 < 0 {
		return nil
	}
	if i1 > i2 {
		e1, e2 = e2, e1
		i1, i2 = i2, i1
	}
	if i2 != i1+1 {
		return nil
	}

	e1Hot := e1.IsHotEdge()
	e2Hot := e2.IsHotEdge()
	samePolyType := e1.Seg.Src == e2.Seg.Src

	// Update winding counts in place — Clipper2 SetWindCount-free incremental
	// model for the non-zero fill rule. Crossing an edge of the same source
	// flips/steps WindSelf; crossing the other source's edge steps WindOther.
	// This is by edge identity (uses e1/e2's WindDx), so the AEL position swap
	// is irrelevant to it.
	if samePolyType {
		if e1.WindSelf+e2.WindDx == 0 {
			e1.WindSelf = -e1.WindSelf
		} else {
			e1.WindSelf += e2.WindDx
		}
		if e2.WindSelf-e1.WindDx == 0 {
			e2.WindSelf = -e2.WindSelf
		} else {
			e2.WindSelf -= e1.WindDx
		}
	} else {
		e1.WindOther += e2.WindDx
		e2.WindOther -= e1.WindDx
	}

	w1 := absInt(e1.WindSelf)
	w2 := absInt(e2.WindSelf)

	// Refresh the Contributing flag for both edges' new winding state, so
	// later events (closeBound, cursor advance) see consistent classification.
	e1.Contributing = isContributing(op, e1)
	e2.Contributing = isContributing(op, e2)

	// Dispatch BEFORE swapping AEL positions: Clipper2 runs IntersectEdges with
	// the AEL still in pre-crossing order and only swaps afterwards
	// (engine.cpp:2461-2462). This matters because AddLocalMinPoly's
	// getPrevHotEdge walks the AEL by position; with e1 still the left edge it
	// finds the genuine enclosing hot edge instead of e2 itself, fixing the
	// crossing-spawned ring's front/back orientation (DESIGN.md §12.11). The
	// swap is performed unconditionally on exit, including the guard path,
	// matching Clipper2's caller which always swaps.
	var result *OutPt
	// Guard: a non-hot edge whose own count is now deeper than the outer
	// boundary (abs > 1) cannot start or close a ring here (engine.cpp:1932).
	// An edge is eligible when it is hot or its count is in {0,1}; dispatch only
	// when BOTH edges are eligible.
	e1Eligible := e1Hot || w1 == 0 || w1 == 1
	e2Eligible := e2Hot || w2 == 0 || w2 == 1
	if e1Eligible && e2Eligible {
		result = dispatchIntersect(ael, op, e1, e2, pt, e1Hot, e2Hot, w1, w2, samePolyType)
	}

	// e1 was the edge left of e2; swap their AEL positions now (the crossing
	// has been processed).
	ael.SwapAt(i1)
	return result
}

func dispatchIntersect(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	e1Hot, e2Hot bool,
	w1, w2 int,
	samePolyType bool,
) *OutPt {
	// A coincident collinear horizontal pair from different sources whose
	// interiors lie on OPPOSITE sides (Seg.Reversed differs) overlaps rather
	// than crosses transversally: the shared edge is interior and cancels
	// (Union) or is one source's boundary (Difference/Intersect). It is NOT a
	// crossing — any ring-op here (AddLocalMaxPoly merge, or AddOutPt+
	// SwapOutrecs) prematurely closes a ring or transfers it onto the other
	// source's coincident edge, dropping the hot bound's continuation. Do
	// nothing (the caller still swaps AEL positions); each bound keeps building
	// its own clean run, and [sweep.processHorzJoins] reconnects the two
	// overlapping runs once the global ring topology is known. Same-side
	// coincident pairs (Reversed equal) are a genuine doubled boundary where one
	// edge is interior, and fall through to normal dispatch. Xor is excluded:
	// its coincident pairs are resolved by the standard maximum handling and it
	// does not run the horz-join pass.
	//
	// The skip applies only when at least one bound terminates at the overlap
	// (IsBoundLast — a local-max plateau), NEITHER bound continues collinearly
	// past the overlap (continuesCollinearHorizontal) AND the pair is not a
	// re-spawn handoff (respawnHandoffAtOverlap). A genuine doubled-boundary
	// cancellation has one polygon's bound turning at the shared edge (its
	// plateau ends there); when BOTH bounds continue past the overlap with
	// sloped/vertical edges the coincident horizontals are two live boundaries
	// that each carry on — crossing them normally is required, so do NOT skip.
	// SplitOverlaps fragments a long horizontal into pieces; when one bound's
	// horizontal ends at the overlap but the other continues past it, the
	// coincident pair is a boundary EXIT, not a mutual cancellation — the
	// continuing bound must re-spawn via the normal one-hot transfer, so do NOT
	// skip there either. (DESIGN.md §12.11.)
	if op != OpXor && !samePolyType &&
		e1.Outrec != e2.Outrec && w1 <= 1 && w2 <= 1 &&
		e1.Seg.Horizontal() && e2.Seg.Horizontal() &&
		e1.Seg.Reversed != e2.Seg.Reversed &&
		max(e1.Seg.Bot.X, e2.Seg.Bot.X) < min(e1.Seg.Top.X, e2.Seg.Top.X) &&
		(e1.IsBoundLast() || e2.IsBoundLast()) &&
		!collinearContinuationBlocksSkip(e1) && !collinearContinuationBlocksSkip(e2) &&
		!respawnHandoffAtOverlap(e1, e2) {
		return nil
	}

	switch {
	case e1Hot && e2Hot:
		return branchBothHot(ael, op, e1, e2, pt, w1, w2, samePolyType)
	case e1Hot:
		result := AddOutPt(e1, pt)
		SwapOutrecs(e1, e2)
		return result
	case e2Hot:
		result := AddOutPt(e2, pt)
		SwapOutrecs(e1, e2)
		return result
	default:
		return branchNeitherHot(ael, op, e1, e2, pt, w1, w2, samePolyType)
	}
}

// continuesCollinearHorizontal reports whether ae's bound continues past its
// current horizontal segment with another collinear horizontal segment at the
// same Y. SplitOverlaps fragments a long horizontal that partially overlaps a
// different-source horizontal into adjacent pieces; when one source's bound
// continues collinearly beyond the coincident overlap while the other's bound
// ends there, the coincident pair is not a mutual cancellation but a boundary
// EXIT — the continuing bound must re-spawn (normal dispatch), not be skipped.
func continuesCollinearHorizontal(ae *ActiveEdge) bool {
	if ae.Bound == nil {
		return false
	}
	next := ae.EdgeIdx + 1
	if next >= len(ae.Bound.Segs) {
		return false
	}
	ns := ae.Bound.Segs[next]
	return ns.Horizontal() && ns.Bot.Y == ae.Seg.Bot.Y
}

// collinearContinuationBlocksSkip reports whether ae's collinear horizontal
// continuation past the coincident overlap should BLOCK the opposite-side skip
// in [dispatchIntersect]. A continuing bound forces normal dispatch (re-spawn)
// ONLY when it is COLD: it carries no ring yet and must pick one up via the
// one-hot SwapOutrecs as it exits the overlap (the corner-exit / SplitOverlaps
// re-spawn case the guard was written for). When the continuing bound is already
// HOT it is mid-build; skipping lets it keep its own clean run across the
// overlap, while the other (ending, cold) coincident edge is a redundant doubled
// boundary. Forcing dispatch there instead transfers the hot ring onto the cold
// dead-end edge and corrupts the topology — the subject-hole-top coincident with
// the clip-top, hole inside clip, which emitted the clip region as a stray
// positive ring instead of a hole (DESIGN.md §12.11).
func collinearContinuationBlocksSkip(ae *ActiveEdge) bool {
	return continuesCollinearHorizontal(ae) && !ae.IsHotEdge()
}

// respawnHandoffAtOverlap reports whether a coincident opposite-side horizontal
// pair is a boundary EXIT whose continuation leaves NON-horizontally (so
// continuesCollinearHorizontal does not catch it). One bound terminates at the
// overlap — its local-max plateau, IsBoundLast — while the other continues past
// with a sloped or vertical segment; the terminating bound is hot and the
// continuing bound is cold. Falling through to the one-hot dispatch then runs
// AddOutPt+SwapOutrecs, transferring the hot ring onto the continuing cold bound
// so it re-spawns — what the corner-exit case needs (A's top horizontal ends at
// a shared apex where A turns vertical; DESIGN.md §12.11). When the continuing
// bound is already hot (a genuine cancellation) the ring is already on the right
// bound and the skip must fire, so this returns false there.
func respawnHandoffAtOverlap(e1, e2 *ActiveEdge) bool {
	l1, l2 := e1.IsBoundLast(), e2.IsBoundLast()
	if l1 == l2 {
		return false
	}
	if l1 {
		return e1.IsHotEdge() && !e2.IsHotEdge()
	}
	return e2.IsHotEdge() && !e1.IsHotEdge()
}

func branchBothHot(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	w1, w2 int,
	samePolyType bool,
) *OutPt {
	// Both rings close together when the wind state is "complicated" — at
	// least one of the edges is deeper than ±1, or different polytype
	// (different inputs meet) except for Xor where they always interleave.
	if w1 > 1 || w2 > 1 || (!samePolyType && op != OpXor) {
		return AddLocalMaxPoly(ael, e1, e2, pt)
	}
	// Coincident horizontal hot edges (identical Bot and Top) do not cross
	// transversally — they are a doubled boundary running along the same
	// segment, produced when two sources' top plateaus overlap (SplitOverlaps
	// fragments them into matching pieces). The tunnel branch below assumes a
	// point-crossing; applying it here joins one ring into the other and
	// respawns a degenerate apex spike, collapsing a coincident-plateau Xor
	// result (e.g. A=(5,9),(3,3),(12,9),(7,9), B=(12,9),(10,9),(2,10),(6,3):
	// Xor 21.15 vs 16.60, the intersection-hole dropped its apex; DESIGN.md
	// §12.11). Fall through to the interleave instead: each ring keeps its own
	// chain along the shared edge and they swap AEL ownership. Restricted to
	// Xor — the other ops route coincident pairs through the opposite-side
	// skip in dispatchIntersect above.
	coincidentHoriz := op == OpXor &&
		e1.Seg.Horizontal() && e2.Seg.Horizontal() &&
		e1.Seg.Bot == e2.Seg.Bot && e1.Seg.Top == e2.Seg.Top
	// Tunnel case: at the intersection, two rings touch at a single point
	// (one closes, an immediately new one opens). Clipper2 detects this
	// with IsFront(e1) || same OutRec — see §12.5.
	if !coincidentHoriz && (e1.IsFront() || e1.Outrec == e2.Outrec) {
		result := AddLocalMaxPoly(ael, e1, e2, pt)
		AddLocalMinPoly(ael, e1, e2, pt, false)
		return result
	}
	// Otherwise the two rings just interleave: each emits a vertex and
	// the rings swap which AEL position they belong to.
	AddOutPt(e1, pt)
	result := AddOutPt(e2, pt)
	SwapOutrecs(e1, e2)
	return result
}

func branchNeitherHot(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	w1, w2 int,
	samePolyType bool,
) *OutPt {
	if !samePolyType {
		return AddLocalMinPoly(ael, e1, e2, pt, false)
	}
	if w1 != 1 || w2 != 1 {
		return nil
	}
	wc2a, wc2b := e1.WindOther, e2.WindOther
	switch op {
	case OpUnion:
		if wc2a <= 0 && wc2b <= 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
	case OpIntersect:
		if wc2a > 0 && wc2b > 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
	case OpDifference:
		if e1.Seg.Src == Clip && wc2a > 0 && wc2b > 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
		if e1.Seg.Src == Subject && wc2a <= 0 && wc2b <= 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
	case OpXor:
		return AddLocalMinPoly(ael, e1, e2, pt, false)
	}
	return nil
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
