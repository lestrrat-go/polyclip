package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func vert(xv int64, src Source, reversed bool) Segment {
	bot := fixed.Point{X: fixed.Coord(xv), Y: 0}
	top := fixed.Point{X: fixed.Coord(xv), Y: 10}
	if reversed {
		return Segment{Bot: bot, Top: top, Src: src, Reversed: true}
	}
	return Segment{Bot: bot, Top: top, Src: src, Reversed: false}
}

func TestClassify(t *testing.T) {
	// Each case builds vertical segments via vert(), optionally sets the fill
	// rule, inserts them into an AEL, classifies each edge with the given
	// operation, then runs its check closure against the resulting edges (in
	// insertion order).
	type segSpec struct {
		x        int64
		src      Source
		reversed bool
	}
	cases := []struct {
		name    string
		fill    FillRule // zero value = FillNonZero
		setFill bool
		op      Operation
		segs    []segSpec
		check   func(t *testing.T, edges []*ActiveEdge)
	}{
		{
			// One subject edge, no neighbors.
			name: "LeftmostEdge",
			op:   OpUnion,
			segs: []segSpec{
				{0, Subject, true}, // Reversed → delta = +1
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				ae := edges[0]
				require.Equal(t, 1, ae.WindSelf, "WindSelf: %d want 1", ae.WindSelf)
				require.Equal(t, 0, ae.WindOther, "WindOther: %d want 0", ae.WindOther)
				require.True(t, ae.Contributing, "leftmost edge should be contributing for Union")
			},
		},
		{
			// CCW subject square: left edge (X=0, downward in input — Reversed)
			// then right edge (X=10, upward — non-Reversed). At Y=5.
			name: "TwoSameSource",
			op:   OpUnion,
			segs: []segSpec{
				{0, Subject, true},   // delta = +1
				{10, Subject, false}, // delta = -1
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				aeL, aeR := edges[0], edges[1]
				// WindSelf is Clipper2's wind_cnt — the HIGHER of the winding counts
				// of the two regions touching the edge — so both sides of the square
				// read 1 (interior region has winding 1; exterior 0). The right edge
				// reverses direction relative to the left, so it inherits the left's
				// count.
				require.True(t, aeL.WindSelf == 1 && aeR.WindSelf == 1, "WindSelf: L=%d R=%d want 1 1", aeL.WindSelf, aeR.WindSelf)
				require.True(t, aeL.Contributing && aeR.Contributing, "Contributing: L=%v R=%v want both true", aeL.Contributing, aeR.Contributing)
			},
		},
		{
			// Two nested same-source squares (outer X=0..20, inner X=5..15), all
			// CCW. Under even-odd every edge bounds the source region regardless of
			// winding magnitude, so ALL four verticals are contributing (the inner
			// pair forms a hole). NonZero would drop the inner pair (WindSelf == 2).
			name:    "EvenOddSameSourceNested",
			fill:    FillEvenOdd,
			setFill: true,
			op:      OpUnion,
			segs: []segSpec{
				{0, Subject, true},   // +1
				{5, Subject, true},   // +1
				{15, Subject, false}, // -1
				{20, Subject, false}, // -1
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				for i, ae := range edges {
					require.Equal(t, 0, ae.WindOther, "edge %d: WindOther=%d want 0 (no clip)", i, ae.WindOther)
					require.True(t, ae.Contributing, "edge %d: not contributing; even-odd treats every edge as a boundary", i)
				}
			},
		},
		{
			// Two nested clip walls to the left, then a subject wall. The subject
			// edge sees two clip crossings → even parity → OUTSIDE the clip under
			// even-odd, so it does NOT contribute to an Intersection (NonZero's
			// WindOther==2 would).
			name:    "EvenOddCrossSourceParity",
			fill:    FillEvenOdd,
			setFill: true,
			op:      OpIntersect,
			segs: []segSpec{
				{0, Clip, true},    // +1
				{2, Clip, true},    // +1
				{5, Subject, true}, // +1
				{8, Clip, false},   // -1
				{10, Clip, false},  // -1
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				sa := edges[2] // the subject wall
				require.Equal(t, 0, sa.WindOther, "subject WindOther=%d want 0 (even clip parity)", sa.WindOther)
				require.False(t, sa.Contributing, "subject wall should NOT contribute to Intersect: even clip parity = outside clip")
			},
		},
		{
			// Subject square at X=0..10, clip square at X=5..15. Both CCW.
			// At a scanline that crosses all four vertical edges:
			//   X=0  subject left  (Reversed → +1)
			//   X=5  clip left     (Reversed → +1)
			//   X=10 subject right (Reversed=false → -1)
			//   X=15 clip right    (Reversed=false → -1)
			name: "TwoOverlappingSquares",
			op:   OpUnion,
			segs: []segSpec{
				{0, Subject, true},
				{5, Clip, true},
				{10, Subject, false},
				{15, Clip, false},
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				// WindSelf is Clipper2's wind_cnt (higher winding of the two adjacent
				// same-source regions), so each square's left and right edges both
				// read 1. Contributing is what actually drives output and is
				// unchanged:
				//   aeSL: WindSelf=1 WindOther=0  contributing (subject outer, outside clip)
				//   aeCL: WindSelf=1 WindOther=1  NOT contributing (clip edge inside subject)
				//   aeSR: WindSelf=1 WindOther=1  NOT contributing (subject edge inside clip)
				//   aeCR: WindSelf=1 WindOther=0  contributing (clip outer, outside subject)
				inner := []struct {
					name        string
					ae          *ActiveEdge
					wantSelf    int
					wantOther   int
					wantContrib bool
				}{
					{"aeSL", edges[0], 1, 0, true},
					{"aeCL", edges[1], 1, 1, false},
					{"aeSR", edges[2], 1, 1, false},
					{"aeCR", edges[3], 1, 0, true},
				}
				for _, c := range inner {
					require.True(t, c.ae.WindSelf == c.wantSelf && c.ae.WindOther == c.wantOther && c.ae.Contributing == c.wantContrib,
						"%s: WindSelf=%d WindOther=%d Contributing=%v; want %d %d %v",
						c.name, c.ae.WindSelf, c.ae.WindOther, c.ae.Contributing,
						c.wantSelf, c.wantOther, c.wantContrib)
				}
			},
		},
		{
			// Same configuration as TwoOverlappingSquares; for Intersect the inside
			// edges contribute and the outside-only edges don't.
			name: "OpIntersect",
			op:   OpIntersect,
			segs: []segSpec{
				{0, Subject, true},
				{5, Clip, true},
				{10, Subject, false},
				{15, Clip, false},
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				// For Intersect: contribute iff WindOther != 0.
				wantContrib := []bool{false, true, true, false}
				for i, w := range wantContrib {
					require.Equal(t, w, edges[i].Contributing, "Intersect[%d].Contributing=%v want %v", i, edges[i].Contributing, w)
				}
			},
		},
		{
			// For Xor every flip contributes regardless of WindOther.
			name: "OpXor",
			op:   OpXor,
			segs: []segSpec{
				{0, Subject, true},
				{5, Clip, true},
				{10, Subject, false},
				{15, Clip, false},
			},
			check: func(t *testing.T, edges []*ActiveEdge) {
				for i, ae := range edges {
					require.True(t, ae.Contributing, "Xor[%d]: not contributing — should be (every flip contributes)", i)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			segs := make([]Segment, len(tc.segs))
			for i, sp := range tc.segs {
				segs[i] = vert(sp.x, sp.src, sp.reversed)
			}
			ael := NewAEL()
			if tc.setFill {
				ael.Fill = tc.fill
			}
			edges := make([]*ActiveEdge, len(segs))
			for i := range segs {
				ae := &ActiveEdge{Seg: &segs[i], CurrX: segs[i].Bot.X, WindDx: signedContribution(&segs[i])}
				ael.Insert(ae)
				edges[i] = ae
			}
			for _, ae := range edges {
				Classify(ael, ae, tc.op)
			}
			tc.check(t, edges)
		})
	}
}

func TestSignedContribution(t *testing.T) {
	cases := []struct {
		name string
		seg  Segment
		want int
	}{
		{"non-reversed (upward input)", Segment{Reversed: false, Bot: fixed.Point{Y: 0}, Top: fixed.Point{Y: 1}}, -1},
		{"reversed (downward input)", Segment{Reversed: true, Bot: fixed.Point{Y: 0}, Top: fixed.Point{Y: 1}}, +1},
		{"horizontal", Segment{Bot: fixed.Point{X: 0, Y: 5}, Top: fixed.Point{X: 10, Y: 5}}, 0},
	}
	for _, c := range cases {
		require.Equal(t, c.want, signedContribution(&c.seg), "%s: want %d", c.name, c.want)
	}
}
