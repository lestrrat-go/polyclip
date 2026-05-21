package clip

import "sort"

// Horizontal-join subsystem — a port of Clipper2's horz_seg_list_ /
// horz_join_list_ machinery (clipper.engine.cpp: AddTrialHorzJoin,
// UpdateHorzSegment, ConvertHorzSegsToJoins, ProcessHorzJoins). See
// DESIGN.md §12.11.
//
// Why it exists: when two collinear horizontal edges from different sources
// overlap over a span, [sweep.doHorizontal] crosses them via [IntersectEdges]
// at their shared near endpoint. Two overlapping horizontals do not cross
// transversally, so that produces a tangled / mis-joined ring. The fix is
// Clipper2's: do NOT decide the splice locally. Instead record every output
// vertex emitted on a horizontal as a "trial" anchor, and at end-of-scanline
// pair up overlapping opposite-direction horizontal runs into deferred joins.
// Those joins are spliced once at end-of-sweep, when the global ring topology
// is known, splitting one ring or merging two as the geometry requires.
//
// polyclip computes hole ownership in postprocess (DESIGN.md §11.9) and eagerly
// re-threads OutPts on every ring merge, so the polytree-only owner/splits
// machinery in Clipper2's ProcessHorzJoins is omitted: a same-ring join is a
// pure split (new OutRec + re-thread), a cross-ring join releases the second
// ring.

// horzSegment is one trial horizontal anchor: the left and right OutPts of a
// maximal run of output vertices sharing the anchor's Y, oriented so leftOp.X
// <= rightOp.X. leftToRight records the ring's traversal direction across the
// run (Clipper2 HorzSegment).
type horzSegment struct {
	leftOp      *OutPt
	rightOp     *OutPt
	leftToRight bool
}

// horzJoin is a confirmed deferred join between two horizontal runs: op1's run
// is spliced into op2's run at end-of-sweep (Clipper2 HorzJoin).
type horzJoin struct {
	op1 *OutPt
	op2 *OutPt
}

// addTrialHorzJoin records op as a trial horizontal-join anchor. Called from
// [sweep.doHorizontal] after every OutPt emitted while traversing a hot
// horizontal (Clipper2 AddTrialHorzJoin, engine.cpp:2494).
func (s *sweep) addTrialHorzJoin(op *OutPt) {
	if op == nil || op.Outrec == nil {
		return
	}
	s.horzSegList = append(s.horzSegList, &horzSegment{leftOp: op})
}

// getLastOp returns the OutPt at the chain end that hotEdge is currently
// building (head if front, head.Next if back). Clipper2 GetLastOp
// (engine.cpp:2485). The op returned by [IntersectEdges] for the horizontal
// may have re-associated to a different ring, so this reads it fresh.
func getLastOp(hotEdge *ActiveEdge) *OutPt {
	outrec := hotEdge.Outrec
	result := outrec.Pts
	if hotEdge != outrec.FrontEdge {
		result = result.Next
	}
	return result
}

// setHorzSegHeadingForward orients hs so leftOp is the lex-smaller-X endpoint
// (Clipper2 SetHorzSegHeadingForward, engine.cpp:2156). Returns false if the
// two ops share an X (a zero-width run, useless for joining).
func setHorzSegHeadingForward(hs *horzSegment, opP, opN *OutPt) bool {
	if opP.P.X == opN.P.X {
		return false
	}
	if opP.P.X < opN.P.X {
		hs.leftOp = opP
		hs.rightOp = opN
		hs.leftToRight = true
		return true
	}
	hs.leftOp = opN
	hs.rightOp = opP
	hs.leftToRight = false
	return true
}

// updateHorzSegment expands hs.leftOp into the maximal run of consecutive
// OutPts at the same Y and records the run's extent and direction. Returns
// false if the run is zero-width or its left anchor is already claimed by
// another segment (Clipper2 UpdateHorzSegment, engine.cpp:2174).
func updateHorzSegment(hs *horzSegment) bool {
	op := hs.leftOp
	outrec := op.Outrec
	outrecHasEdges := outrec.FrontEdge != nil
	currY := op.P.Y
	opP, opN := op, op
	if outrecHasEdges {
		opA := outrec.Pts
		opZ := opA.Next
		for opP != opZ && opP.Prev.P.Y == currY {
			opP = opP.Prev
		}
		for opN != opA && opN.Next.P.Y == currY {
			opN = opN.Next
		}
	} else {
		for opP.Prev != opN && opP.Prev.P.Y == currY {
			opP = opP.Prev
		}
		for opN.Next != opP && opN.Next.P.Y == currY {
			opN = opN.Next
		}
	}
	result := setHorzSegHeadingForward(hs, opP, opN) && hs.leftOp.horz == nil
	if result {
		hs.leftOp.horz = hs
	} else {
		hs.rightOp = nil // mark as dead for sorting
	}
	return result
}

// convertHorzSegsToJoins pairs every overlapping, opposite-direction pair of
// this scanline's trial horizontal segments into a deferred [horzJoin], then
// clears the trial list. Called at end-of-scanline (Clipper2
// ConvertHorzSegsToJoins, engine.cpp:2207).
func (s *sweep) convertHorzSegsToJoins() {
	// Expand each trial anchor into its maximal same-Y run; count the live
	// (joinable) ones. updateHorzSegment marks dead segments with rightOp==nil.
	j := 0
	for _, hs := range s.horzSegList {
		if updateHorzSegment(hs) {
			j++
		}
	}
	// Clear claim markers before returning early or after pairing, so a later
	// scanline's segments start fresh.
	defer func() {
		for _, hs := range s.horzSegList {
			if hs.leftOp != nil {
				hs.leftOp.horz = nil
			}
		}
		s.horzSegList = s.horzSegList[:0]
	}()
	if j < 2 {
		return
	}

	// Stable-sort live segments to the front by leftOp.X; dead (rightOp==nil)
	// sink to the back. Mirrors Clipper2 HorzSegSorter + the count-if partition.
	segs := make([]*horzSegment, len(s.horzSegList))
	copy(segs, s.horzSegList)
	sort.SliceStable(segs, func(a, b int) bool {
		ha, hb := segs[a], segs[b]
		if ha.rightOp == nil || hb.rightOp == nil {
			return ha.rightOp != nil // live before dead
		}
		return ha.leftOp.P.X < hb.leftOp.P.X
	})

	for i := 0; i < j-1; i++ {
		hs1 := segs[i]
		for k := i + 1; k < j; k++ {
			hs2 := segs[k]
			if hs2.leftOp.P.X >= hs1.rightOp.P.X ||
				hs2.leftToRight == hs1.leftToRight ||
				hs2.rightOp.P.X <= hs1.leftOp.P.X {
				continue
			}
			currY := hs1.leftOp.P.Y
			if hs1.leftToRight {
				for hs1.leftOp.Next.P.Y == currY && hs1.leftOp.Next.P.X <= hs2.leftOp.P.X {
					hs1.leftOp = hs1.leftOp.Next
				}
				for hs2.leftOp.Prev.P.Y == currY && hs2.leftOp.Prev.P.X <= hs1.leftOp.P.X {
					hs2.leftOp = hs2.leftOp.Prev
				}
				s.horzJoinList = append(s.horzJoinList, horzJoin{
					op1: duplicateOp(hs1.leftOp, true),
					op2: duplicateOp(hs2.leftOp, false),
				})
			} else {
				for hs1.leftOp.Prev.P.Y == currY && hs1.leftOp.Prev.P.X <= hs2.leftOp.P.X {
					hs1.leftOp = hs1.leftOp.Prev
				}
				for hs2.leftOp.Next.P.Y == currY && hs2.leftOp.Next.P.X <= hs1.leftOp.P.X {
					hs2.leftOp = hs2.leftOp.Next
				}
				s.horzJoinList = append(s.horzJoinList, horzJoin{
					op1: duplicateOp(hs2.leftOp, true),
					op2: duplicateOp(hs1.leftOp, false),
				})
			}
		}
	}
}

// processHorzJoins splices every deferred [horzJoin] at end-of-sweep. A join
// whose two anchors are in the same ring splits that ring in two; a join across
// two rings merges them. Clipper2 ProcessHorzJoins (engine.cpp:2268), reduced
// to the non-polytree case (owner/splits handling omitted — see file header).
func (s *sweep) processHorzJoins() {
	for _, j := range s.horzJoinList {
		or1 := j.op1.Outrec
		or2 := j.op2.Outrec

		op1b := j.op1.Next
		op2b := j.op2.Prev
		j.op1.Next = j.op2
		j.op2.Prev = j.op1
		op1b.Prev = op2b
		op2b.Next = op1b

		if or1 == or2 {
			// A "join" within one ring is really a split into two rings.
			or2 = s.ael.newOutRec()
			or2.Pts = op1b
			fixOutRecPts(or2)
			if or1.Pts.Outrec == or2 {
				or1.Pts = j.op1
				or1.Pts.Outrec = or1
			}
			continue
		}
		// Merge: op2's ring is absorbed into op1's. Re-thread the now-unified
		// cycle onto or1 (polyclip's eager model) and release or2.
		for op := op1b; ; op = op.Next {
			op.Outrec = or1
			if op == j.op1 {
				break
			}
		}
		or2.Pts = nil
		or2.FrontEdge = nil
		or2.BackEdge = nil
	}
	s.horzJoinList = s.horzJoinList[:0]
}

// duplicateOp inserts a copy of op adjacent to it in the ring chain (after op
// when insertAfter, else before) and returns the copy. Clipper2 DuplicateOp
// (engine.cpp:278).
func duplicateOp(op *OutPt, insertAfter bool) *OutPt {
	result := &OutPt{P: op.P, Outrec: op.Outrec}
	if insertAfter {
		result.Next = op.Next
		result.Next.Prev = result
		result.Prev = op
		op.Next = result
	} else {
		result.Prev = op.Prev
		result.Prev.Next = result
		result.Next = op
		op.Prev = result
	}
	return result
}

// fixOutRecPts re-threads every OutPt in outrec's cycle to point at outrec
// (Clipper2 FixOutRecPts, engine.cpp:2147).
func fixOutRecPts(outrec *OutRec) {
	op := outrec.Pts
	for {
		op.Outrec = outrec
		op = op.Next
		if op == outrec.Pts {
			break
		}
	}
}

// newOutRec allocates and registers a fresh OutRec with a stable index.
func (a *AEL) newOutRec() *OutRec {
	or := &OutRec{Idx: a.NextOutRecIdx()}
	a.RegisterRing(or)
	return or
}
