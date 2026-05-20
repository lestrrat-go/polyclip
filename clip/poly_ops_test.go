package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

// makeClassifiedEdge returns an ActiveEdge with pre-set classification state
// (WindSelf, WindOther, Contributing). Used to exercise IntersectEdges
// branches in isolation.
func makeClassifiedEdge(currX int64, src Source, windSelf, windOther int) *ActiveEdge {
	bot := fixed.Point{X: fixed.Coord(currX), Y: 0}
	top := fixed.Point{X: fixed.Coord(currX), Y: 100}
	seg := &Segment{Bot: bot, Top: top, Src: src, Reversed: src == Subject}
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
	// Two cold edges of the SAME source cross, with WindOther=0 on both.
	// Branch C, same polytype, w1==w2==1, op=Union, w1c2<=0 && w2c2<=0
	// → AddLocalMinPoly.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	e2 := makeClassifiedEdge(10, Subject, 1, 0)
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
	// Same polytype, wind 1, op=Intersect: contribute only if w1c2>0 && w2c2>0.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 1)
	e2 := makeClassifiedEdge(10, Subject, 1, 1)
	ael.Insert(e1)
	ael.Insert(e2)
	if op := IntersectEdges(ael, OpIntersect, e1, e2, fixed.Point{X: 5, Y: 50}); op == nil {
		t.Errorf("Intersect with both inside other should emit")
	}
}

func TestIntersectEdgesBranchCXor(t *testing.T) {
	// Same polytype, wind 1, op=Xor: always contribute.
	ael := NewAEL()
	e1 := makeClassifiedEdge(0, Subject, 1, 0)
	e2 := makeClassifiedEdge(10, Subject, 1, 0)
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
