package clip

import "github.com/lestrrat-go/polyclip/fixed"

// IntersectEdges applies DESIGN.md §12.5's decision tree at an intersection
// of two adjacent AEL edges. It captures the pre-swap classification state,
// performs the AEL swap, emits output via [AddLocalMinPoly], [AddLocalMaxPoly],
// [AddOutPt], or [SwapOutrecs] as the table dictates, and re-classifies the
// two edges for their new positions.
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

	// Snapshot pre-swap classification state — §12.5 dispatches on the
	// *old* winding counts and ring identity.
	e1Hot := e1.IsHotEdge()
	e2Hot := e2.IsHotEdge()
	oldW1 := e1.WindSelf
	oldW2 := e2.WindSelf
	oldW1c2 := e1.WindOther
	oldW2c2 := e2.WindOther
	samePolyType := e1.Seg.Src == e2.Seg.Src

	// Perform the AEL swap (intersection event semantics).
	ael.SwapAt(i1)

	result := dispatchIntersect(
		ael, op,
		e1, e2, pt,
		e1Hot, e2Hot,
		oldW1, oldW2, oldW1c2, oldW2c2,
		samePolyType,
	)

	// After the swap and any side effects, re-classify both edges for their
	// new AEL positions and for future events. Their post-swap predecessors
	// have changed, so WindSelf / WindOther / Contributing must be refreshed.
	Classify(ael, ael.At(i1), op)
	Classify(ael, ael.At(i1+1), op)
	return result
}

func dispatchIntersect(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	e1Hot, e2Hot bool,
	oldW1, oldW2, oldW1c2, oldW2c2 int,
	samePolyType bool,
) *OutPt {
	switch {
	case e1Hot && e2Hot:
		return branchBothHot(ael, op, e1, e2, pt, oldW1, oldW2, samePolyType)
	case e1Hot:
		result := AddOutPt(e1, pt)
		SwapOutrecs(e1, e2)
		return result
	case e2Hot:
		result := AddOutPt(e2, pt)
		SwapOutrecs(e1, e2)
		return result
	default:
		return branchNeitherHot(ael, op, e1, e2, pt, oldW1, oldW2, oldW1c2, oldW2c2, samePolyType)
	}
}

func branchBothHot(
	ael *AEL, op Operation,
	e1, e2 *ActiveEdge, pt fixed.Point,
	oldW1, oldW2 int,
	samePolyType bool,
) *OutPt {
	// Both rings close together when the wind state is "complicated" — at
	// least one of the edges is deeper than ±1, or different polytype
	// (different inputs meet) except for Xor where they always interleave.
	if absInt(oldW1) > 1 || absInt(oldW2) > 1 || (!samePolyType && op != OpXor) {
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
	oldW1, oldW2, oldW1c2, oldW2c2 int,
	samePolyType bool,
) *OutPt {
	if !samePolyType {
		return AddLocalMinPoly(ael, e1, e2, pt, false)
	}
	if oldW1 != 1 || oldW2 != 1 {
		return nil
	}
	switch op {
	case OpUnion:
		if oldW1c2 <= 0 && oldW2c2 <= 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
	case OpIntersect:
		if oldW1c2 > 0 && oldW2c2 > 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
	case OpDifference:
		if e1.Seg.Src == Clip && oldW1c2 > 0 && oldW2c2 > 0 {
			return AddLocalMinPoly(ael, e1, e2, pt, false)
		}
		if e1.Seg.Src == Subject && oldW1c2 <= 0 && oldW2c2 <= 0 {
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
