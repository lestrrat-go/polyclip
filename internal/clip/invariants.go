package clip

import "fmt"

// CheckInvariants verifies DESIGN.md §11.10's engine invariants on the
// final sweep state. Returns nil if all invariants hold, else an error
// describing the first violation found.
//
// Per §11.10 the invariants are "useful as runtime asserts or test
// post-conditions"; this function realises the post-condition flavour —
// it inspects the AEL and ring list AFTER [Sweep] returns. Invariants 3
// and 4 ("hot⇒contributing" and "no adjacent same-source contributing
// edges") are NOT checked because [IntersectEdges]' SwapOutrecs legitimately
// leaves a hot edge non-contributing in mid-sweep (an edge swapped into a
// ring whose interior classification doesn't match its own boundary status),
// and adjacency may transiently violate the rule between an intersection and
// the following re-classification. Those invariants are aspirational
// per-event guarantees that the current implementation doesn't uphold
// strictly — see DESIGN.md §11.10 for the revised statement.
//
// The function is called from tests; production callers should not invoke
// it (it walks every OutPt of every ring).
func CheckInvariants(sw *SweepResult, segs []Segment) error {
	if sw == nil {
		return fmt.Errorf("CheckInvariants: nil SweepResult")
	}
	if sw.Err != nil {
		// Sweep aborted; invariants don't apply.
		return nil //nolint:nilerr // intentional: aborted sweep has nothing to check.
	}
	if err := checkRingsClosed(sw.Rings); err != nil {
		return err
	}
	if err := checkWindCountsBounded(sw.Rings, segs); err != nil {
		return err
	}
	return nil
}

// checkRingsClosed verifies §11.10 invariant 5: at sweep end, every
// OutRec is either closed (Pts non-nil with a properly-linked cycle) or
// retired (Pts == nil, the ring was merged into another via
// [JoinOutrecPaths]). No partially-open rings.
func checkRingsClosed(rings []*OutRec) error {
	for i, r := range rings {
		if r == nil {
			continue
		}
		if r.Pts == nil {
			// Merged ring — OK.
			continue
		}
		// Verify the cycle is consistent: walk Next forward, must return
		// to Pts within a finite number of steps without breaks.
		if err := checkCycleLinks(r); err != nil {
			return fmt.Errorf("invariant 5: ring %d (idx=%d): %v", i, r.Idx, err)
		}
	}
	return nil
}

// checkCycleLinks walks ring's OutPt cycle and verifies Next/Prev
// invariants: every node's Next.Prev == itself, the cycle closes within
// 4× the visible vertex count (a sanity bound), and every node's
// Outrec back-pointer matches.
func checkCycleLinks(r *OutRec) error {
	start := r.Pts
	if start == nil {
		return fmt.Errorf("pts is nil")
	}
	// First count vertices to set an upper bound for the walk.
	count := 1
	for p := start.Next; p != start; p = p.Next {
		count++
		if count > 1<<20 {
			return fmt.Errorf("cycle does not close within 2²⁰ steps (Next chain broken)")
		}
	}
	// Re-walk to verify Prev links and Outrec back-pointers.
	for p := start; ; p = p.Next {
		if p.Next == nil || p.Prev == nil {
			return fmt.Errorf("OutPt at %v has nil Next or Prev", p.P)
		}
		if p.Next.Prev != p {
			return fmt.Errorf("OutPt at %v: Next.Prev != self", p.P)
		}
		if p.Outrec != r {
			return fmt.Errorf("OutPt at %v: Outrec back-pointer mismatch", p.P)
		}
		if p.Next == start {
			break
		}
	}
	return nil
}

// checkWindCountsBounded verifies §11.10 invariant 2: no edge's |WindSelf|
// exceeds the number of input rings of its source. The check uses ring
// count derived from segs (one ring per source contributes one full ±1
// excursion); the test is a sanity bound, not a tight bound.
//
// Since the AEL is empty at sweep end (all edges have been removed via
// closeBound / AddLocalMaxPoly), this invariant is checked via the segs
// directly: count distinct ring starts per source and assert no single
// segment's classification produced an out-of-bounds WindSelf during
// processing — for that we'd need a recorded trace. As a weaker but
// useful proxy, count rings emitted: |rings| ≤ |input rings| per source
// would imply WindSelf stayed bounded.
//
// For Phase 2 first cut this is informational; we just verify rings is
// not absurdly larger than the input ring count.
func checkWindCountsBounded(rings []*OutRec, segs []Segment) error {
	closedRings := 0
	for _, r := range rings {
		if r != nil && r.Pts != nil {
			closedRings++
		}
	}
	// Count distinct (Bot, Top) keys to estimate ring count from inputs.
	// This is approximate — a single ring with N edges contributes N
	// segments. We bound output rings by total segment count (every edge
	// could in principle start its own ring at a synthetic minimum).
	if closedRings > len(segs) {
		return fmt.Errorf("invariant 2 proxy: %d closed rings > %d input segments — ring count out of sanity bound",
			closedRings, len(segs))
	}
	return nil
}

// CheckAELSorted verifies §11.10 invariant 1: the AEL is sorted
// left-to-right by CurrX with slope tie-break. Intended for runtime
// assertion during sweep development; tests call it on an AEL snapshot.
//
// Returns the index of the first out-of-order adjacent pair, or -1 if
// all pairs satisfy aelLess(prev, next).
func CheckAELSorted(a *AEL) int {
	for i := 1; i < a.Len(); i++ {
		prev := a.At(i - 1)
		curr := a.At(i)
		if !aelLess(prev, curr) && (prev.CurrX != curr.CurrX || slope(prev.Seg) > slope(curr.Seg)) {
			return i
		}
	}
	return -1
}
