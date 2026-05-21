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
// The ael parameter is accepted (and ignored) for symmetry with
// [AddLocalMinPoly]; future increments will use it when implementing the
// open-end recovery path.
func AddLocalMaxPoly(_ *AEL, e1, e2 *ActiveEdge, pt fixed.Point) *OutPt {
	if e1.IsFront() == e2.IsFront() {
		if e1.Outrec == e2.Outrec {
			// Same ring, same side — a genuine inconsistency; bail.
			return nil
		}
		// Two different rings meeting same-side at a maximum. In Clipper2 this
		// is unreachable for closed paths (SwapFrontBackSides is reserved for
		// open ends; the closed-path branch is a hard error) — it means a
		// crossing-spawned ring upstream got an inverted front/back orientation.
		// AddLocalMinPoly resolving orientation from AEL position (DESIGN.md
		// §12.11) and the shared-vertex crossing dispatch
		// ([sweep.reconcileSharedVertexCrossings]) drove firings to ZERO for
		// Union/Intersect/Difference over the simple-quad differential; the only
		// op that still reaches here is Xor, and those firings belong to the
		// separately-tracked Xor classification gap (DESIGN.md §12.11). Replacing
		// this branch with `return nil` (Clipper2's hard-error) currently leaves
		// the whole suite green and the differential wrong-rates unchanged, so
		// the recovery is kept only as a safety net until the Xor track lands.
		// Recover by reversing e2's ring sides so the polarities oppose, then
		// join. This mirrors Clipper2's SwapFrontBackSides (engine.cpp:460), which
		// ALSO advances the Pts head by one — the chain head is the front vertex
		// (Pts) with the back at Pts.Next, so swapping which edge is front must
		// move the head to keep [AddOutPt] and [JoinOutrecPaths] splicing on the
		// correct ends. Omitting the head shift tangles the merged chain.
		e2.Outrec.FrontEdge, e2.Outrec.BackEdge = e2.Outrec.BackEdge, e2.Outrec.FrontEdge
		e2.Outrec.Pts = e2.Outrec.Pts.Next
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
