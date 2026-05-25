package clip

import "github.com/lestrrat-go/polyclip/internal/fixed"

// FillRule selects which polygon-fill convention decides whether a winding
// count is "inside". The boolean ops always use [FillNonZero]; [FillPositive]
// / [FillNegative] are used only by the single-source self-union that cleans a
// self-intersecting offset ring (offset.go), where the sign of a sub-loop's
// winding distinguishes the kept region from a spurious overshoot fold.
type FillRule int

const (
	// FillNonZero fills a region whose winding count is non-zero (abs == 1 on
	// an outer boundary edge). The only rule used by the boolean ops.
	FillNonZero FillRule = iota
	// FillPositive fills a region with strictly positive winding; an outer
	// boundary edge has WindSelf == +1. Negative (clockwise) sub-loops are
	// dropped.
	FillPositive
	// FillNegative fills a region with strictly negative winding; an outer
	// boundary edge has WindSelf == -1.
	FillNegative
	// FillEvenOdd fills a region crossed by an odd number of edges. Every edge
	// is a source boundary regardless of winding magnitude (the WindSelf test in
	// [isContributing] is skipped), and the other source's membership is counted
	// by crossing parity rather than signed winding. Used for self-overlapping
	// or self-intersecting inputs where the doubled regions of [FillNonZero]
	// should read as holes.
	FillEvenOdd
)

// Operation is the boolean set operation requested by the caller.
type Operation int

const (
	// OpUnion computes the set-theoretic union (a ∪ b).
	OpUnion Operation = iota
	// OpIntersect computes the intersection (a ∩ b). Reserved for Phase 3.
	OpIntersect
	// OpDifference computes a ∖ b. Reserved for Phase 3.
	OpDifference
	// OpXor computes the symmetric difference. Reserved for Phase 3.
	OpXor
)

// Classify computes ae.WindSelf, ae.WindOther, and ae.Contributing for the
// active edge that has just been inserted in ael, against the given boolean
// operation. It is the insertion-time winding setup only; once an edge is in
// the AEL its counts are updated incrementally by [IntersectEdges], never by
// re-running Classify.
//
// This transcribes Clipper2's SetWindCountForClosedPathEdge (engine.cpp:1011)
// for the non-zero fill rule. WindSelf (Clipper2's wind_cnt) is the winding
// count of ae's own source in the region just right of ae; WindOther
// (wind_cnt2) is the running sum of the other source's signed contributions
// to ae's left. The naive "nearest predecessor + delta" model this replaced
// dropped the reversing-direction and now-outside cases, which is what made
// front/back polarity drift at intersections (DESIGN.md §12.11).
func Classify(ael *AEL, ae *ActiveEdge, op Operation) {
	pos := ael.IndexOf(ae)
	if pos < 0 {
		return
	}
	delta := ae.WindDx

	// Positive/Negative fill (the single-source self-union) use a pure signed
	// prefix sum for WindSelf — the winding of the region immediately right of
	// ae is the signed count of same-source edges at or left of ae. Unlike the
	// NonZero "reversing direction" model below, this represents a doubled
	// coincident wall faithfully as a two-step +1 → 0 → -1 transition, so a
	// self-overlapping offset ring resolves without the multi-frame vote
	// (DESIGN.md §7.2). NonZero keeps Clipper2's incremental model unchanged.
	if ael.Ordered {
		// Order edges by their position just ABOVE the current scanline, not at
		// it: a bound whose cursor sits on a LEADING horizontal (a local-min
		// notch/plateau edge) is physically at the horizontal's near X now but
		// continues at its far end (its first non-horizontal wall). The winding
		// of the region the new ring lives in is the y+ε winding, so the prefix
		// sum must place such a bound at its far X. Without this the doubled-wall
		// degeneracy mis-assigns the sliver winding and a spurious ring spawns
		// (DESIGN.md §7.2, the L3 simultaneous-spawn case).
		myX := effectiveX(ae)
		sum := ae.WindDx
		for i := range ael.Len() {
			e := ael.At(i)
			if e == ae || e.Seg.Src != ae.Seg.Src {
				continue
			}
			ex := effectiveX(e)
			if ex < myX || (ex == myX && i < pos) {
				sum += e.WindDx
			}
		}
		ae.WindSelf = sum
		ae.WindOther = 0
		ae.Contributing = isContributing(ael.Fill, ael.Ordered, op, ae)
		return
	}

	// Even-odd fill (Clipper2 SetWindCountForClosedPathEdge, engine.cpp:1028):
	// WindSelf is just the edge direction (every edge is a source boundary; the
	// magnitude test is skipped in isContributing), and WindOther is the PARITY
	// of the other source's edges to ae's left (each crossing toggles inside/
	// outside) rather than a signed sum.
	if ael.Fill == FillEvenOdd {
		ae.WindSelf = delta
		other := 0
		for i := range pos {
			if prev := ael.At(i); prev.Seg.Src != ae.Seg.Src {
				other ^= 1
			}
		}
		ae.WindOther = other
		ae.Contributing = isContributing(ael.Fill, ael.Ordered, op, ae)
		return
	}

	// Nearest same-source predecessor (Clipper2's e2).
	var e2 *ActiveEdge
	for i := pos - 1; i >= 0; i-- {
		if prev := ael.At(i); prev.Seg.Src == ae.Seg.Src {
			e2 = prev
			break
		}
	}

	switch {
	case e2 == nil:
		ae.WindSelf = delta
	case e2.WindSelf*e2.WindDx < 0:
		// Opposite directions: ae is outside e2.
		if absInt(e2.WindSelf) > 1 {
			if e2.WindDx*delta < 0 {
				ae.WindSelf = e2.WindSelf // reversing direction
			} else {
				ae.WindSelf = e2.WindSelf + delta
			}
		} else {
			ae.WindSelf = delta // now outside all polys of same source
		}
	default:
		// ae is inside e2.
		if e2.WindDx*delta < 0 {
			ae.WindSelf = e2.WindSelf // reversing direction
		} else {
			ae.WindSelf = e2.WindSelf + delta
		}
	}

	// WindOther = sum of the other source's signed contributions to ae's left.
	other := 0
	for i := range pos {
		if prev := ael.At(i); prev.Seg.Src != ae.Seg.Src {
			other += prev.WindDx
		}
	}
	ae.WindOther = other

	ae.Contributing = isContributing(ael.Fill, ael.Ordered, op, ae)
}

// signedContribution returns the AEL contribution of seg per the convention
// in DESIGN.md §11.3:
//
//   - non-reversed (input direction matches canonical Bot→Top, i.e. upward):
//     contributes -1 to a left-to-right winding-count ray
//   - reversed (input direction is Top→Bot, i.e. downward): contributes +1
//   - horizontal: contributes 0 (horizontals never enter the AEL)
//
// Under this convention a CCW outer ring produces an interior winding of +1
// and a CW hole produces -1, consistent with the standard non-zero winding
// rule.
func signedContribution(seg *Segment) int {
	if seg.Horizontal() {
		return 0
	}
	if seg.Reversed {
		return +1
	}
	return -1
}

// isContributing reports whether ae bounds an output region for op, given its
// current WindSelf/WindOther. This transcribes Clipper2's IsContributingClosed
// (engine.cpp:908) for the non-zero fill rule: the edge must be on the outer
// boundary of its own source (the WindSelf test selected by fill — abs==1 for
// NonZero, ==+1 for Positive, ==-1 for Negative), and the other source's count
// must satisfy the operation's inside/outside test. The WindOther test stays
// NonZero because Positive/Negative are used only by the single-source
// self-union where WindOther is identically zero. ae must already have
// WindSelf and WindOther set.
//
// When ordered is set (the [SweepRingsFill] ordered-minima path) the
// Positive/Negative test is the winding-`>0` BOUNDARY form rather than the
// exact `WindSelf == ±1`: a doubled coincident wall's true boundary edge has
// WindSelf == 0 on its right (the `+1 → 0` step), which the exact test drops
// (DESIGN.md §7.2). The soup/vote path leaves ordered false and keeps the
// exact test.
func isContributing(fill FillRule, ordered bool, op Operation, ae *ActiveEdge) bool {
	switch fill {
	case FillEvenOdd:
		// Every edge bounds its own source's region (crossing parity), so there is
		// no WindSelf magnitude test; membership is decided entirely by the WindOther
		// parity test below (Clipper2 IsContributingClosed, engine.cpp:912).
	case FillPositive:
		if ordered {
			if (ae.WindSelf > 0) == (ae.WindSelf-ae.WindDx > 0) {
				return false
			}
		} else if ae.WindSelf != 1 {
			return false
		}
	case FillNegative:
		if ordered {
			if (ae.WindSelf < 0) == (ae.WindSelf-ae.WindDx < 0) {
				return false
			}
		} else if ae.WindSelf != -1 {
			return false
		}
	default: // FillNonZero
		if absInt(ae.WindSelf) != 1 {
			return false
		}
	}
	switch op {
	case OpUnion:
		return ae.WindOther == 0
	case OpIntersect:
		return ae.WindOther != 0
	case OpXor:
		return true
	case OpDifference:
		// Subject edge: contribute when clip count is 0; clip edge: when
		// subject count is non-zero.
		if ae.Seg.Src == Subject {
			return ae.WindOther == 0
		}
		return ae.WindOther != 0
	}
	return false
}

// effectiveX returns the X at which ae's bound sits just above the current
// scanline: for a cursor on a horizontal segment, the far end of the
// horizontal (where the bound's next non-horizontal edge continues upward);
// otherwise the edge's current crossing X. Used by the ordered positive-fill
// winding prefix sum so a leading-horizontal bound is ordered where its wall
// actually lies (DESIGN.md §7.2).
func effectiveX(ae *ActiveEdge) fixed.Coord {
	if ae.Seg.Horizontal() && ae.Bound != nil {
		return boundHorizontalFarX(ae.Bound, ae.Seg)
	}
	return ae.CurrX
}
