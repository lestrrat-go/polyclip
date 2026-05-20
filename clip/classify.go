package clip

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

	ae.Contributing = isContributing(op, ae)
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
// boundary of its own source (abs(WindSelf) == 1), and the other source's
// count must satisfy the operation's inside/outside test. ae must already have
// WindSelf and WindOther set.
func isContributing(op Operation, ae *ActiveEdge) bool {
	if absInt(ae.WindSelf) != 1 {
		return false
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
