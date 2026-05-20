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

	// Perform the AEL swap (intersection event semantics). e1 is the edge that
	// was left of e2; their winding update below is by edge identity, not
	// position, so it is unaffected by the swap.
	ael.SwapAt(i1)

	// Update winding counts in place — Clipper2 SetWindCount-free incremental
	// model for the non-zero fill rule. Crossing an edge of the same source
	// flips/steps WindSelf; crossing the other source's edge steps WindOther.
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

	// Guard: a non-hot edge whose own count is now deeper than the outer
	// boundary (abs > 1) cannot start or close a ring here. (Clipper2
	// engine.cpp:1932 — the swap has already happened, matching its caller.)
	if (!e1Hot && w1 != 0 && w1 != 1) || (!e2Hot && w2 != 0 && w2 != 1) {
		return nil
	}

	return dispatchIntersect(ael, op, e1, e2, pt, e1Hot, e2Hot, w1, w2, samePolyType)
}

func dispatchIntersect(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	e1Hot, e2Hot bool,
	w1, w2 int,
	samePolyType bool,
) *OutPt {
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
	// Tunnel case: at the intersection, two rings touch at a single point
	// (one closes, an immediately new one opens). Clipper2 detects this
	// with IsFront(e1) || same OutRec — see §12.5.
	if e1.IsFront() || e1.Outrec == e2.Outrec {
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
