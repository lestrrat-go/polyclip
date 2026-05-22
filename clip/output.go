package clip

import "github.com/lestrrat-go/polyclip/fixed"

// OutPt is one vertex on an output ring's doubly-linked cyclic list.
//
// The cycle invariant is: every OutPt has non-nil Next and Prev, and following
// Next from any vertex eventually returns to the same vertex.
//
// A single-vertex ring is represented as a one-node cycle where Next == Prev
// == this OutPt.
type OutPt struct {
	P      fixed.Point
	Next   *OutPt
	Prev   *OutPt
	Outrec *OutRec

	// horz marks this OutPt as the claimed left anchor of a horizontal
	// segment during [sweep.convertHorzSegsToJoins] (Clipper2 OutPt::horz).
	// Transient; nil outside that pass.
	horz *horzSegment
}

// OutRec ("output record") is an output ring under construction or closed.
// Per DESIGN.md §12.2.
type OutRec struct {
	// Idx is a stable identifier used to break ties when two rings merge in
	// JoinOutrecPaths (the lower-indexed ring is preserved).
	Idx int

	// FrontEdge and BackEdge are the two active edges currently building this
	// ring. FrontEdge contributes to the head of the chain (prepend) and
	// BackEdge to the tail (append). Both are nil once the ring is closed.
	FrontEdge *ActiveEdge
	BackEdge  *ActiveEdge

	// Pts points at one vertex of the ring's cycle (the head while the ring
	// is open). Walking Next from Pts visits every vertex of the ring exactly
	// once before returning to Pts.
	//
	// nil when the ring has been merged into another (released).
	Pts *OutPt

	// Owner is set by postprocess for hole-assignment.
	Owner *OutRec

	// IsHole is set by postprocess from the ring's signed area.
	IsHole bool
}

// IsHotEdge reports whether ae is currently building a ring.
func (ae *ActiveEdge) IsHotEdge() bool { return ae.Outrec != nil }

// IsFront reports whether ae is the front edge of its OutRec. Panics if
// ae is not hot.
func (ae *ActiveEdge) IsFront() bool { return ae.Outrec.FrontEdge == ae }

// AddOutPt appends pt to ae's ring chain on the appropriate side (head if
// ae is the front edge, tail otherwise). Returns the new OutPt.
//
// If pt equals the existing head or tail (depending on side), the function
// returns the existing OutPt without appending — this dedupes consecutive
// identical vertices.
//
// Per DESIGN.md §12.2 / clipper.engine.cpp:1497.
func AddOutPt(ae *ActiveEdge, pt fixed.Point) *OutPt {
	outrec := ae.Outrec
	toFront := ae.IsFront()
	opFront := outrec.Pts
	opBack := opFront.Next

	if toFront && pt == opFront.P {
		return opFront
	}
	if !toFront && pt == opBack.P {
		return opBack
	}

	newOp := &OutPt{P: pt, Outrec: outrec}
	opBack.Prev = newOp
	newOp.Prev = opFront
	newOp.Next = opBack
	opFront.Next = newOp
	if toFront {
		outrec.Pts = newOp
	}
	return newOp
}

// AddLocalMinPoly creates a new OutRec rooted at pt and assigns the two edges
// as its sides. isNew is true for input local minima and false for synthetic
// minima from IntersectEdges' crossing case. Returns the new ring's first
// OutPt.
//
// Per DESIGN.md §12.3 / clipper.engine.cpp:1332. The front/back assignment
// transcribes Clipper2's SetSides logic, which is expressed in terms of the
// LEFT and RIGHT AEL edges (front_edge is the ascending/left side for a simple
// outer minimum). The two arguments may be passed in either AEL order; this
// function resolves left/right from their current AEL positions so the
// orientation — and crucially getPrevHotEdge, which must find the enclosing
// hot edge to the left of BOTH new edges rather than the partner — matches
// Clipper2 regardless of caller argument order.
func AddLocalMinPoly(ael *AEL, e1, e2 *ActiveEdge, pt fixed.Point, isNew bool) *OutPt {
	outrec := &OutRec{Idx: ael.NextOutRecIdx()}
	ael.RegisterRing(outrec)
	e1.Outrec = outrec
	e2.Outrec = outrec

	left, right := e1, e2
	if ael.IndexOf(e2) < ael.IndexOf(e1) {
		left, right = e2, e1
	}

	// getPrevHotEdge must find the hot edge enclosing this minimum from the
	// LEFT — i.e. the nearest hot edge strictly left of BOTH new edges, never
	// the partner. Walk from the left edge so the right edge (just made hot)
	// is never returned. polyclip uses the mirror of Clipper2's orientation
	// (FrontEdge = the RIGHT/descending side, so the Pts cycle reads CCW), so
	// the front side is the right edge for a simple outer minimum and the
	// nesting parity is read off outrecIsAscending in that same mirror.
	prevHot := getPrevHotEdge(ael, left)
	frontIsRight := isNew
	if prevHot != nil {
		frontIsRight = outrecIsAscending(prevHot) == isNew
	}
	if frontIsRight {
		outrec.FrontEdge = right
		outrec.BackEdge = left
	} else {
		outrec.FrontEdge = left
		outrec.BackEdge = right
	}

	op := &OutPt{P: pt, Outrec: outrec}
	op.Next = op
	op.Prev = op
	outrec.Pts = op
	return op
}

// AddLocalMaxPoly closes (or merges) the ring(s) of e1 and e2, which are
// meeting at a local maximum pt. Returns the OutPt added at pt, or nil if
// the configuration is invalid (e.g. both edges are the same side).
//
// If e1 and e2 belong to the same OutRec, the ring is closed and both edges
// are uncoupled. If they belong to different OutRecs, [JoinOutrecPaths]
// splices them into one.
//
// Per DESIGN.md §12.4 / clipper.engine.cpp:1380.
//
// The ael parameter is used by the same-side recovery below to read the
// geometric left/right order of the two edges at the maximum.
func AddLocalMaxPoly(ael *AEL, e1, e2 *ActiveEdge, pt fixed.Point) *OutPt {
	if e1.IsFront() == e2.IsFront() {
		if e1.Outrec == e2.Outrec {
			// Same ring, same side — a genuine inconsistency; bail.
			return nil
		}
		// Two different rings meeting SAME-side at a maximum. This is the
		// vertex-on-edge degeneracy (DESIGN.md §12.11): polyclip's bottom-up
		// sweep builds two interleaved rings where a top-down sweep would build
		// several disjoint ones, and they arrive at the apex both-front. A
		// front/back-relabel + JoinOutrecPaths cannot splice two same-front
		// chains without folding the apex into a degenerate out-and-back spike
		// (the apex's true neighbours — e.g. the two edges of the apex triangle —
		// collapse onto one point), losing that region's area.
		//
		// Instead, merge the two cycles as a figure-8 PINCH at the apex: emit the
		// apex on each ring, then cross-link the two apex OutPts so the two cycles
		// become ONE self-touching walk that revisits the apex. assembleResult's
		// splitSelfTouchingRings then decomposes the merged walk back into the
		// correct simple rings at every revisited vertex. No orientation guess is
		// needed — the decomposition is read off the geometry afterwards.
		//
		// This is only valid when neither ring has a CONTINUING other edge — i.e.
		// each ring's other side has already left the AEL (a terminal maximum
		// where both ends meet, the vertex-on-edge apex case): the figure-8 closes
		// both rings completely. If either ring still has an active other edge in
		// the AEL (a JOIN that keeps the merged ring open — e.g. the
		// tunnel/different-polytype cases), that edge must keep ownership and a
		// live Pts chain, so fall back to the relabel + JoinOutrecPaths path.
		continuing := func(e *ActiveEdge) bool {
			o := otherEdge(e)
			return o != nil && ael.IndexOf(o) >= 0
		}
		if !continuing(e1) && !continuing(e2) {
			op1 := AddOutPt(e1, pt)
			op2 := AddOutPt(e2, pt)
			n1, n2 := op1.Next, op2.Next
			op1.Next, n2.Prev = n2, op1
			op2.Next, n1.Prev = n1, op2
			or1, or2 := e1.Outrec, e2.Outrec
			for p := op1.Next; ; p = p.Next {
				p.Outrec = or1
				if p == op1 {
					break
				}
			}
			or1.Pts = op1
			or1.FrontEdge, or1.BackEdge = nil, nil
			or2.FrontEdge, or2.BackEdge = nil, nil
			or2.Pts = nil
			e1.Outrec, e2.Outrec = nil, nil
			return op1
		}
		// Exactly one ring continues, and both maximum edges are FRONT: the
		// terminal ring is a complete loop while the continuing ring survives on
		// its BACK edge. The relabel + JoinOutrecPaths path below folds the apex
		// into a degenerate out-and-back spike here (the same-front splice
		// collapses the apex triangle, dropping its area — DESIGN.md §12.11,
		// vertex-on-edge apex under a continuing bound). Instead splice the
		// terminal loop into the continuing ring as a self-touching detour at the
		// apex (the figure-8 cross-link), but KEEP the continuing ring's back edge
		// and its growing tip: set Pts to the terminal apex node so Pts.Next is the
		// continuing back edge's tip (preserved by the cross-link). The merged
		// walk revisits the apex; splitSelfTouchingRings decomposes it later.
		c1, c2 := continuing(e1), continuing(e2)
		if c1 != c2 && e1.IsFront() && e2.IsFront() {
			cont, term := e1, e2
			if c2 {
				cont, term = e2, e1
			}
			opC := AddOutPt(cont, pt)
			opT := AddOutPt(term, pt)
			nC, nT := opC.Next, opT.Next
			opC.Next, nT.Prev = nT, opC
			opT.Next, nC.Prev = nC, opT
			orC, orT := cont.Outrec, term.Outrec
			for p := opC; ; p = p.Next {
				p.Outrec = orC
				if p.Next == opC {
					break
				}
			}
			orC.Pts = opT // Pts.Next == nC == continuing back edge's tip
			orC.FrontEdge = nil
			orT.FrontEdge, orT.BackEdge = nil, nil
			orT.Pts = nil
			cont.Outrec, term.Outrec = nil, nil
			return opC
		}
		// Fallback recovery for same-side JOINs with a continuing edge: relabel
		// the inverted ring's sides (mirror of Clipper2 SwapFrontBackSides) so the
		// subsequent JoinOutrecPaths splices on the correct ends.
		swapSides := func(e *ActiveEdge) {
			e.Outrec.FrontEdge, e.Outrec.BackEdge = e.Outrec.BackEdge, e.Outrec.FrontEdge
			e.Outrec.Pts = e.Outrec.Pts.Next
		}
		i1, i2 := ael.IndexOf(e1), ael.IndexOf(e2)
		if i1 < 0 || i2 < 0 {
			swapSides(e2)
		} else {
			left, right := e1, e2
			if i1 > i2 {
				left, right = e2, e1
			}
			if !right.IsFront() {
				swapSides(right)
			} else if left.IsFront() {
				swapSides(left)
			}
		}
	}
	result := AddOutPt(e1, pt)
	if e1.Outrec == e2.Outrec {
		outrec := e1.Outrec
		outrec.Pts = result
		outrec.FrontEdge = nil
		outrec.BackEdge = nil
		e1.Outrec = nil
		e2.Outrec = nil
		return result
	}
	if e1.Outrec.Idx < e2.Outrec.Idx {
		JoinOutrecPaths(e1, e2)
	} else {
		JoinOutrecPaths(e2, e1)
	}
	return result
}

// JoinOutrecPaths splices e2's OutRec chain onto e1's, then discards e2's
// OutRec. Both edges are uncoupled from their (now unified) ring.
//
// Per DESIGN.md §12.4 / clipper.engine.cpp:1435.
func JoinOutrecPaths(e1, e2 *ActiveEdge) {
	p1st := e1.Outrec.Pts
	p2st := e2.Outrec.Pts
	p1end := p1st.Next
	p2end := p2st.Next

	if e1.IsFront() {
		p2end.Prev = p1st
		p1st.Next = p2end
		p2st.Next = p1end
		p1end.Prev = p2st
		e1.Outrec.Pts = p2st
		e1.Outrec.FrontEdge = e2.Outrec.FrontEdge
		if e1.Outrec.FrontEdge != nil {
			e1.Outrec.FrontEdge.Outrec = e1.Outrec
		}
	} else {
		p1end.Prev = p2st
		p2st.Next = p1end
		p1st.Next = p2end
		p2end.Prev = p1st
		e1.Outrec.BackEdge = e2.Outrec.BackEdge
		if e1.Outrec.BackEdge != nil {
			e1.Outrec.BackEdge.Outrec = e1.Outrec
		}
	}
	// Re-thread every OutPt in the merged chain to point at e1.Outrec.
	for op := e1.Outrec.Pts; ; op = op.Next {
		op.Outrec = e1.Outrec
		if op.Next == e1.Outrec.Pts {
			break
		}
	}
	e2.Outrec.FrontEdge = nil
	e2.Outrec.BackEdge = nil
	e2.Outrec.Pts = nil
	e1.Outrec = nil
	e2.Outrec = nil
}

// SwapOutrecs swaps which OutRec each edge belongs to, used at certain
// intersection configurations where neither ring closes but the two rings'
// edge ownership crosses (clipper.engine.cpp:336).
func SwapOutrecs(e1, e2 *ActiveEdge) {
	or1 := e1.Outrec
	or2 := e2.Outrec
	if or1 == or2 {
		// Same ring — swap front and back.
		if or1 != nil {
			or1.FrontEdge, or1.BackEdge = or1.BackEdge, or1.FrontEdge
		}
		return
	}
	if or1 != nil {
		if e1 == or1.FrontEdge {
			or1.FrontEdge = e2
		} else {
			or1.BackEdge = e2
		}
	}
	if or2 != nil {
		if e2 == or2.FrontEdge {
			or2.FrontEdge = e1
		} else {
			or2.BackEdge = e1
		}
	}
	e1.Outrec, e2.Outrec = or2, or1
}

// Points returns the ring's vertices in cycle order starting from r.Pts.
// Returns nil for an unreleased or empty ring.
func (r *OutRec) Points() []fixed.Point {
	if r == nil || r.Pts == nil {
		return nil
	}
	out := []fixed.Point{r.Pts.P}
	for p := r.Pts.Next; p != r.Pts; p = p.Next {
		out = append(out, p.P)
	}
	return out
}

// getPrevHotEdge walks left from ae looking for the nearest hot edge.
// Returns nil if no hot edge is found to the left.
func getPrevHotEdge(ael *AEL, ae *ActiveEdge) *ActiveEdge {
	pos := ael.IndexOf(ae)
	for i := pos - 1; i >= 0; i-- {
		prev := ael.At(i)
		if prev.IsHotEdge() {
			return prev
		}
	}
	return nil
}

// outrecIsAscending reports whether hotEdge is the front edge of its OutRec
// (clipper.engine.cpp:455).
func outrecIsAscending(hotEdge *ActiveEdge) bool {
	return hotEdge.Outrec.FrontEdge == hotEdge
}

// otherEdge returns e's ring's OTHER active edge (the one that is not e), or
// nil if that side has already closed or e is not coupled as a side.
func otherEdge(e *ActiveEdge) *ActiveEdge {
	or := e.Outrec
	if or == nil {
		return nil
	}
	if or.FrontEdge == e {
		return or.BackEdge
	}
	if or.BackEdge == e {
		return or.FrontEdge
	}
	return nil
}
