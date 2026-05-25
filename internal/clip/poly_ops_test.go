package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
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

func TestIntersectEdges(t *testing.T) {
	type edgeSpec struct {
		currX     int64
		src       Source
		reversed  bool // only consulted when rev is true
		rev       bool // build via makeClassifiedEdgeRev instead of makeClassifiedEdge
		windSelf  int
		windOther int
	}
	type testcase struct {
		name string
		// extra edges (besides e1/e2) to insert into the AEL before the call.
		extra []edgeSpec
		e1    edgeSpec
		e2    edgeSpec
		// callE2, when non-nil, is built and inserted but the IntersectEdges
		// call uses it as the second edge instead of e2 (for stale/non-adjacent
		// cases). When stale is set, callE2 is built but NOT inserted.
		callE2  *edgeSpec
		stale   bool
		op      Operation
		point   fixed.Point
		wantNil bool
		// assert, when non-nil, runs extra state assertions on the result and
		// edges after the call.
		assert func(t *testing.T, op *OutPt, e1, e2 *ActiveEdge)
	}

	build := func(s edgeSpec) *ActiveEdge {
		if s.rev {
			return makeClassifiedEdgeRev(s.currX, s.src, s.reversed, s.windSelf, s.windOther)
		}
		return makeClassifiedEdge(s.currX, s.src, s.windSelf, s.windOther)
	}

	cases := []testcase{
		{
			name:    "StaleEventDropped",
			e1:      edgeSpec{currX: 0, src: Subject, windSelf: 1, windOther: 0},
			e2:      edgeSpec{currX: 10, src: Clip, windSelf: 1, windOther: 0},
			callE2:  &edgeSpec{currX: 100, src: Subject, windSelf: 1, windOther: 0},
			stale:   true, // e3 (callE2) is not inserted into the AEL
			op:      OpUnion,
			point:   fixed.Point{X: 50, Y: 5},
			wantNil: true,
		},
		{
			name:    "NonAdjacentDropped",
			e1:      edgeSpec{currX: 0, src: Subject, windSelf: 1, windOther: 0},
			e2:      edgeSpec{currX: 10, src: Clip, windSelf: 1, windOther: 1},
			callE2:  &edgeSpec{currX: 20, src: Subject, windSelf: 0, windOther: 1},
			op:      OpUnion,
			point:   fixed.Point{X: 15, Y: 5},
			wantNil: true,
		},
		{
			// Two cold edges of different sources cross.
			// Branch C, different polytype → AddLocalMinPoly always.
			name:  "BranchCUnionFresh",
			e1:    edgeSpec{currX: 0, src: Subject, windSelf: 1, windOther: 0},
			e2:    edgeSpec{currX: 10, src: Clip, windSelf: 1, windOther: 0},
			op:    OpUnion,
			point: fixed.Point{X: 5, Y: 50},
			assert: func(t *testing.T, op *OutPt, e1, e2 *ActiveEdge) {
				pt := fixed.Point{X: 5, Y: 50}
				require.Equal(t, pt, op.P, "op.P = %v want %v", op.P, pt)
				require.True(t, e1.IsHotEdge() && e2.IsHotEdge(), "edges should be hot after AddLocalMinPoly")
			},
		},
		{
			// Two cold edges of the SAME source converge: the left one is a
			// left-side edge (reversed, WindDx +1) and the right one a
			// right-side edge (non-reversed, WindDx -1), both at WindSelf 1
			// with WindOther 0. The incremental wind update negates both to
			// ±1, so branch C fires for Union (w1==w2==1, WindOther<=0 on
			// both) → AddLocalMinPoly.
			name:  "BranchCUnionSamePolyType",
			e1:    edgeSpec{currX: 0, src: Subject, rev: true, reversed: true, windSelf: 1, windOther: 0},
			e2:    edgeSpec{currX: 10, src: Subject, rev: true, reversed: false, windSelf: 1, windOther: 0},
			op:    OpUnion,
			point: fixed.Point{X: 5, Y: 50},
		},
		{
			// Same polytype, wind 1, but both edges are inside the OTHER
			// source (WindOther > 0). Union absorbs them — no contribution.
			name:    "BranchCUnionInsideOther",
			e1:      edgeSpec{currX: 0, src: Subject, windSelf: 1, windOther: 1},
			e2:      edgeSpec{currX: 10, src: Subject, windSelf: 1, windOther: 1},
			op:      OpUnion,
			point:   fixed.Point{X: 5, Y: 50},
			wantNil: true,
		},
		{
			// Same polytype, converging (opposite WindDx) so both negate to
			// ±1; both inside the other source (WindOther=1). op=Intersect
			// contributes when WindOther>0 on both.
			name:  "BranchCIntersect",
			e1:    edgeSpec{currX: 0, src: Subject, rev: true, reversed: true, windSelf: 1, windOther: 1},
			e2:    edgeSpec{currX: 10, src: Subject, rev: true, reversed: false, windSelf: 1, windOther: 1},
			op:    OpIntersect,
			point: fixed.Point{X: 5, Y: 50},
		},
		{
			// Same polytype, converging (opposite WindDx) so both negate to
			// ±1. op=Xor always contributes when w1==w2==1.
			name:  "BranchCXor",
			e1:    edgeSpec{currX: 0, src: Subject, rev: true, reversed: true, windSelf: 1, windOther: 0},
			e2:    edgeSpec{currX: 10, src: Subject, rev: true, reversed: false, windSelf: 1, windOther: 0},
			op:    OpXor,
			point: fixed.Point{X: 5, Y: 50},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ael := NewAEL()
			e1 := build(tc.e1)
			e2 := build(tc.e2)
			ael.Insert(e1)
			ael.Insert(e2)
			for _, es := range tc.extra {
				ael.Insert(build(es))
			}

			// The second edge passed to IntersectEdges. Defaults to e2 unless
			// the case overrides it with callE2.
			callE2 := e2
			if tc.callE2 != nil {
				callE2 = build(*tc.callE2)
				if !tc.stale {
					ael.Insert(callE2)
				}
			}

			op := IntersectEdges(ael, tc.op, e1, callE2, tc.point)
			if tc.wantNil {
				require.Nil(t, op, "expected nil result, got %+v", op)
				return
			}
			require.NotNil(t, op, "expected IntersectEdges to emit a vertex")
			if tc.assert != nil {
				tc.assert(t, op, e1, callE2)
			}
		})
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
	require.True(t, e1.IsHotEdge(), "setup: e1 should be hot")

	e2 := makeClassifiedEdge(10, Clip, 1, 1)
	ael.Insert(e2)

	pt := fixed.Point{X: 5, Y: 50}
	op := IntersectEdges(ael, OpUnion, e1, e2, pt)
	require.NotNil(t, op, "expected AddOutPt to emit")
	require.Equal(t, pt, op.P, "op.P = %v want %v", op.P, pt)
	// After SwapOutrecs, e2 should now hold the OutRec that was e1's.
	require.True(t, e2.IsHotEdge(), "e2 should have inherited the OutRec via SwapOutrecs")
	require.False(t, e1.IsHotEdge(), "e1 should have lost its OutRec via SwapOutrecs")
}
