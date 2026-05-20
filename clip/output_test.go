package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

// makeEdge returns an ActiveEdge with a synthetic Segment whose canonical
// direction matches the (reversed) flag.
func makeEdge(currX int64, src Source, reversed bool) *ActiveEdge {
	bot := fixed.Point{X: fixed.Coord(currX), Y: 0}
	top := fixed.Point{X: fixed.Coord(currX), Y: 10}
	seg := &Segment{Bot: bot, Top: top, Src: src, Reversed: reversed}
	return &ActiveEdge{
		Seg:    seg,
		CurrX:  fixed.Coord(currX),
		WindDx: signedContribution(seg),
	}
}

func TestAddLocalMinPolyCreatesRing(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	ael.Insert(e1)
	ael.Insert(e2)

	pt := fixed.Point{X: 5, Y: 0}
	op := AddLocalMinPoly(ael, e1, e2, pt, true)

	if op == nil {
		t.Fatal("nil op")
	}
	if op.P != pt {
		t.Errorf("op.P = %v want %v", op.P, pt)
	}
	if op.Next != op || op.Prev != op {
		t.Errorf("single-vertex cycle broken: Next=%p Prev=%p self=%p", op.Next, op.Prev, op)
	}
	if e1.Outrec == nil || e2.Outrec == nil || e1.Outrec != e2.Outrec {
		t.Fatalf("Outrec not shared: e1=%p e2=%p", e1.Outrec, e2.Outrec)
	}
	outrec := e1.Outrec
	if outrec.Pts != op {
		t.Errorf("outrec.Pts != op")
	}
	// With no prior hot edge and isNew=true the FrontEdge is the RIGHT-side
	// edge (e2, at x=10), so the Pts cycle reads CCW (polyclip's mirror of
	// Clipper2's convention — see AddLocalMinPoly). Orientation is resolved
	// from AEL position, not argument order.
	if outrec.FrontEdge != e2 || outrec.BackEdge != e1 {
		t.Errorf("sides wrong: front=%p back=%p (want front=e2=%p back=e1=%p)",
			outrec.FrontEdge, outrec.BackEdge, e2, e1)
	}
	if !e1.IsHotEdge() || !e2.IsHotEdge() {
		t.Errorf("edges should be hot")
	}
	if e1.IsFront() || !e2.IsFront() {
		t.Errorf("IsFront: e1=%v e2=%v want false true", e1.IsFront(), e2.IsFront())
	}
}

func TestAddOutPtAppendsAndPrepends(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	ael.Insert(e1)
	ael.Insert(e2)

	startPt := fixed.Point{X: 5, Y: 0}
	AddLocalMinPoly(ael, e1, e2, startPt, true)
	// e2 (right, x=10) is the front edge; e1 (left) is the back edge.

	// Add a point via e2 (front) — should prepend (become new head).
	p1 := fixed.Point{X: 0, Y: 5}
	newFront := AddOutPt(e2, p1)
	if e2.Outrec.Pts != newFront {
		t.Errorf("front add did not update Pts to new head")
	}

	// Add a point via e1 (back) — should append (head stays put).
	p2 := fixed.Point{X: 10, Y: 5}
	oldHead := e1.Outrec.Pts
	AddOutPt(e1, p2)
	if e1.Outrec.Pts != oldHead {
		t.Errorf("back add changed the head pointer")
	}

	// Walk the cycle: should visit 3 distinct points in some order.
	pts := e1.Outrec.Points()
	if len(pts) != 3 {
		t.Fatalf("ring size: %d want 3 (pts=%v)", len(pts), pts)
	}
}

func TestAddOutPtDedupsConsecutive(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	ael.Insert(e1)
	ael.Insert(e2)

	startPt := fixed.Point{X: 5, Y: 0}
	op := AddLocalMinPoly(ael, e1, e2, startPt, true)

	// Adding the same point on the front side should return the existing head.
	got := AddOutPt(e1, startPt)
	if got != op {
		t.Errorf("front dedup failed: got=%p want=%p", got, op)
	}
	if len(e1.Outrec.Points()) != 1 {
		t.Errorf("ring grew on dedup: %v", e1.Outrec.Points())
	}
}

func TestAddLocalMaxPolySameRingCloses(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	ael.Insert(e1)
	ael.Insert(e2)

	minPt := fixed.Point{X: 5, Y: 0}
	AddLocalMinPoly(ael, e1, e2, minPt, true)

	// Now close at a local maximum.
	maxPt := fixed.Point{X: 5, Y: 10}
	result := AddLocalMaxPoly(ael, e1, e2, maxPt)
	if result == nil {
		t.Fatal("AddLocalMaxPoly returned nil")
	}
	if e1.Outrec != nil || e2.Outrec != nil {
		t.Errorf("edges not uncoupled after close")
	}
}

func TestSwapOutrecsAcrossTwoRings(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	e3 := makeEdge(20, Subject, true)
	e4 := makeEdge(30, Subject, false)
	for _, e := range []*ActiveEdge{e1, e2, e3, e4} {
		ael.Insert(e)
	}
	AddLocalMinPoly(ael, e1, e2, fixed.Point{X: 5, Y: 0}, true)
	AddLocalMinPoly(ael, e3, e4, fixed.Point{X: 25, Y: 0}, true)

	or1, or2 := e1.Outrec, e3.Outrec
	if or1 == or2 {
		t.Fatal("test setup: rings should be distinct")
	}
	SwapOutrecs(e1, e3)
	if e1.Outrec != or2 || e3.Outrec != or1 {
		t.Errorf("SwapOutrecs did not exchange: e1.Outrec=%p e3.Outrec=%p want %p %p",
			e1.Outrec, e3.Outrec, or2, or1)
	}
	// e1 and e3 are the BACK edges of their rings (front = right edge, e2/e4),
	// so the swap updates the BackEdge pointers.
	if or1.BackEdge != e3 {
		t.Errorf("or1.BackEdge = %p want %p", or1.BackEdge, e3)
	}
	if or2.BackEdge != e1 {
		t.Errorf("or2.BackEdge = %p want %p", or2.BackEdge, e1)
	}
}

func TestSwapOutrecsSameRingSwapsSides(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	ael.Insert(e1)
	ael.Insert(e2)
	AddLocalMinPoly(ael, e1, e2, fixed.Point{X: 5, Y: 0}, true)

	originalFront, originalBack := e1.Outrec.FrontEdge, e1.Outrec.BackEdge
	SwapOutrecs(e1, e2)
	if e1.Outrec.FrontEdge != originalBack || e1.Outrec.BackEdge != originalFront {
		t.Errorf("same-ring swap did not swap front/back")
	}
}

func TestJoinOutrecPathsMerges(t *testing.T) {
	ael := NewAEL()
	e1 := makeEdge(0, Subject, true)
	e2 := makeEdge(10, Subject, false)
	e3 := makeEdge(20, Subject, true)
	e4 := makeEdge(30, Subject, false)
	for _, e := range []*ActiveEdge{e1, e2, e3, e4} {
		ael.Insert(e)
	}
	AddLocalMinPoly(ael, e1, e2, fixed.Point{X: 5, Y: 0}, true)
	AddLocalMinPoly(ael, e3, e4, fixed.Point{X: 25, Y: 0}, true)
	AddOutPt(e1, fixed.Point{X: 0, Y: 5})
	AddOutPt(e2, fixed.Point{X: 10, Y: 5})
	AddOutPt(e3, fixed.Point{X: 20, Y: 5})
	AddOutPt(e4, fixed.Point{X: 30, Y: 5})

	or1, or2 := e1.Outrec, e3.Outrec
	originalCount := len(or1.Points()) + len(or2.Points())

	// Join e2 with e3 (front of or1 won't apply; e2 is back). Choose
	// e1.Idx < e2.Idx semantics — call with the lower-idx outrec as e1
	// per AddLocalMaxPoly's protocol.
	JoinOutrecPaths(e2, e3)

	merged := or1.Points()
	if len(merged) != originalCount {
		t.Errorf("merged ring size: %d want %d", len(merged), originalCount)
	}
	if or2.Pts != nil {
		t.Errorf("or2.Pts should be nil after merge")
	}
}
