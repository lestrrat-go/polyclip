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
// active edge that has just been inserted (or repositioned) in ael, against
// the given boolean operation.
//
// Classification follows the rules in DESIGN.md §11.3 / §11.4: walk the AEL
// leftward from ae's current position to find the nearest predecessor of
// the same source and of the other source, take their WindSelf values, and
// add ae's own signed contribution to derive WindSelf. The contribution
// rule from §11.4 then sets Contributing.
func Classify(ael *AEL, ae *ActiveEdge, op Operation) {
	pos := ael.IndexOf(ae)
	if pos < 0 {
		return
	}
	delta := signedContribution(ae.Seg)

	prevSelf, prevOther := 0, 0
	foundSelf, foundOther := false, false
	for i := pos - 1; i >= 0; i-- {
		prev := ael.At(i)
		switch {
		case prev.Seg.Src == ae.Seg.Src && !foundSelf:
			prevSelf = prev.WindSelf
			foundSelf = true
		case prev.Seg.Src != ae.Seg.Src && !foundOther:
			prevOther = prev.WindSelf
			foundOther = true
		}
		if foundSelf && foundOther {
			break
		}
	}

	ae.WindSelf = prevSelf + delta
	ae.WindOther = prevOther
	ae.Contributing = isContributing(op, ae, delta)
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

// isContributing applies the classification table from DESIGN.md §11.4 for
// the given operation. ae must already have WindSelf and WindOther set;
// delta is its signed contribution.
func isContributing(op Operation, ae *ActiveEdge, delta int) bool {
	before := ae.WindSelf - delta
	flips := (before == 0) != (ae.WindSelf == 0)
	switch op {
	case OpUnion:
		return ae.WindOther == 0 && flips
	case OpIntersect:
		return ae.WindOther != 0 && flips
	case OpXor:
		return flips
	case OpDifference:
		// Subject edge: contribute when clip count is 0; clip edge: when
		// subject count is non-zero. Reserved until Phase 3 tests cover it.
		if ae.Seg.Src == Subject {
			return ae.WindOther == 0 && flips
		}
		return ae.WindOther != 0 && flips
	}
	return false
}
