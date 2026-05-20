package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

// makeClassifiedEdge returns an ActiveEdge with pre-set classification state
// (WindSelf, WindOther, Contributing). Used to exercise IntersectEdges
// branches in isolation.
func makeClassifiedEdge(currX int64, src Source, windSelf, windOther int) *ActiveEdge {
	return makeClassifiedEdgeRev(currX, src, src == Subject, windSelf, windOther)
}

// makeClassifiedEdgeRev is like makeClassifiedEdge but lets the caller pick the
// edge's input direction (reversed = WindDx +1, non-reversed = WindDx -1), so a
// test can build a physically consistent crossing where the two converging
// edges of one source have opposite WindDx.
func makeClassifiedEdgeRev(currX int64, src Source, reversed bool, windSelf, windOther int) *ActiveEdge {
	bot := fixed.Point{X: fixed.Coord(currX), Y: 0}
	top := fixed.Point{X: fixed.Coord(currX), Y: 100}
	seg := &Segment{Bot: bot, Top: top, Src: src, Reversed: reversed}
	return &ActiveEdge{
		Seg:       seg,
		CurrX:     fixed.Coord(currX),
		WindDx:    signedContribution(seg),
		WindSelf:  windSelf,
		WindOther: windOther,
	}
}

func TestIntersectEdgesStaleEventDropped(t *testing.T) {
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	e2 := makeClassifiedEdge(10, Clip, 1, 0)
	ael.Insert(e1)
	ael.Insert(e2)
	// e3 is not in the AEL — stale event.
	e3 := makeClassifiedEdge(100, Subject, 1, 0)
	if got := IntersectEdges(ael, OpUnion, e1, e3, fixed.Point{X: 50, Y: 5}); got != nil {
		t.Errorf("stale event should return nil, got %+v", got)
	}
}

func TestIntersectEdgesNonAdjacentDropped(t *testing.T) {
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	e2 := makeClassifiedEdge(10, Clip, 1, 1)
	e3 := makeClassifiedEdge(20, Subject, 0, 1)
	ael.Insert(e1)
	ael.Insert(e2)
	ael.Insert(e3)
	if got := IntersectEdges(ael, OpUnion, e1, e3, fixed.Point{X: 15, Y: 5}); got != nil {
		t.Error("non-adjacent edges should return nil")
	}
}

func TestIntersectEdgesBranchCUnionFresh(t *testing.T) {
	// Two cold edges of different sources cross.
	// Branch C, different polytype → AddLocalMinPoly always.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	e2 := makeClassifiedEdge(10, Clip, 1, 0)
	ael.Insert(e1)
	ael.Insert(e2)

	pt := fixed.Point{X: 5, Y: 50}
	op := IntersectEdges(ael, OpUnion, e1, e2, pt)
	if op == nil {
		t.Fatal("expected AddLocalMinPoly to emit a vertex")
	}
	if op.P != pt {
		t.Errorf("op.P = %v want %v", op.P, pt)
	}
	if !e1.IsHotEdge() || !e2.IsHotEdge() {
		t.Errorf("edges should be hot after AddLocalMinPoly")
	}
}

func TestIntersectEdgesBranchCUnionSamePolyType(t *testing.T) {
	// Two cold edges of the SAME source converge: the left one is a left-side
	// edge (reversed, WindDx +1) and the right one a right-side edge
	// (non-reversed, WindDx -1), both at WindSelf 1 with WindOther 0. The
	// incremental wind update negates both to ±1, so branch C fires for Union
	// (w1==w2==1, WindOther<=0 on both) → AddLocalMinPoly.
	ael := NewAEL()
	e1 := makeClassifiedEdgeRev(0, Subject, true, 1, 0)
	e2 := makeClassifiedEdgeRev(10, Subject, false, 1, 0)
	ael.Insert(e1)
	ael.Insert(e2)

	op := IntersectEdges(ael, OpUnion, e1, e2, fixed.Point{X: 5, Y: 50})
	if op == nil {
		t.Fatal("expected AddLocalMinPoly")
	}
}

func TestIntersectEdgesBranchCUnionInsideOther(t *testing.T) {
	// Same polytype, wind 1, but both edges are inside the OTHER source
	// (WindOther > 0). Union absorbs them — no contribution.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 1)
	e2 := makeClassifiedEdge(10, Subject, 1, 1)
	ael.Insert(e1)
	ael.Insert(e2)

	if op := IntersectEdges(ael, OpUnion, e1, e2, fixed.Point{X: 5, Y: 50}); op != nil {
		t.Errorf("expected no emission, got %+v", op)
	}
}

func TestIntersectEdgesBranchCIntersect(t *testing.T) {
	// Same polytype, converging (opposite WindDx) so both negate to ±1; both
	// inside the other source (WindOther=1). op=Intersect contributes when
	// WindOther>0 on both.
	ael := NewAEL()
	e1 := makeClassifiedEdgeRev(0, Subject, true, 1, 1)
	e2 := makeClassifiedEdgeRev(10, Subject, false, 1, 1)
	ael.Insert(e1)
	ael.Insert(e2)
	if op := IntersectEdges(ael, OpIntersect, e1, e2, fixed.Point{X: 5, Y: 50}); op == nil {
		t.Errorf("Intersect with both inside other should emit")
	}
}

func TestIntersectEdgesBranchCXor(t *testing.T) {
	// Same polytype, converging (opposite WindDx) so both negate to ±1.
	// op=Xor always contributes when w1==w2==1.
	ael := NewAEL()
	e1 := makeClassifiedEdgeRev(0, Subject, true, 1, 0)
	e2 := makeClassifiedEdgeRev(10, Subject, false, 1, 0)
	ael.Insert(e1)
	ael.Insert(e2)
	if op := IntersectEdges(ael, OpXor, e1, e2, fixed.Point{X: 5, Y: 50}); op == nil {
		t.Errorf("Xor should always emit at intersection")
	}
}

func TestIntersectEdgesBranchBOneHot(t *testing.T) {
	// e1 is hot (set up by AddLocalMinPoly), e2 is not. Branch B fires:
	// AddOutPt to e1 + SwapOutrecs.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	helperFront := makeClassifiedEdge(-10, Subject, 1, 0)
	ael.Insert(helperFront)
	ael.Insert(e1)
	AddLocalMinPoly(ael, helperFront, e1, fixed.Point{X: 0, Y: 0}, true)
	if !e1.IsHotEdge() {
		t.Fatal("setup: e1 should be hot")
	}

	e2 := makeClassifiedEdge(10, Clip, 1, 1)
	ael.Insert(e2)

	pt := fixed.Point{X: 5, Y: 50}
	op := IntersectEdges(ael, OpUnion, e1, e2, pt)
	if op == nil {
		t.Fatal("expected AddOutPt to emit")
	}
	if op.P != pt {
		t.Errorf("op.P = %v want %v", op.P, pt)
	}
	// After SwapOutrecs, e2 should now hold the OutRec that was e1's.
	if !e2.IsHotEdge() {
		t.Error("e2 should have inherited the OutRec via SwapOutrecs")
	}
	if e1.IsHotEdge() {
		t.Error("e1 should have lost its OutRec via SwapOutrecs")
	}
}
