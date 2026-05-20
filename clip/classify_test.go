package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

func vert(xv int64, src Source, reversed bool) Segment {
	bot := fixed.Point{X: fixed.Coord(xv), Y: 0}
	top := fixed.Point{X: fixed.Coord(xv), Y: 10}
	if reversed {
		return Segment{Bot: bot, Top: top, Src: src, Reversed: true}
	}
	return Segment{Bot: bot, Top: top, Src: src, Reversed: false}
}

func TestClassifyLeftmostEdge(t *testing.T) {
	// One subject edge, no neighbors.
	s := vert(0, Subject, true) // Reversed → delta = +1
	ael := NewAEL()
	ae := &ActiveEdge{Seg: &s, CurrX: 0, WindDx: signedContribution(&s)}
	ael.Insert(ae)
	Classify(ael, ae, OpUnion)
	if ae.WindSelf != 1 {
		t.Errorf("WindSelf: %d want 1", ae.WindSelf)
	}
	if ae.WindOther != 0 {
		t.Errorf("WindOther: %d want 0", ae.WindOther)
	}
	if !ae.Contributing {
		t.Error("leftmost edge should be contributing for Union")
	}
}

func TestClassifyTwoSameSource(t *testing.T) {
	// CCW subject square: left edge (X=0, downward in input — Reversed)
	// then right edge (X=10, upward — non-Reversed). At Y=5.
	left := vert(0, Subject, true)    // delta = +1
	right := vert(10, Subject, false) // delta = -1
	ael := NewAEL()
	aeL := &ActiveEdge{Seg: &left, CurrX: 0, WindDx: signedContribution(&left)}
	aeR := &ActiveEdge{Seg: &right, CurrX: 10, WindDx: signedContribution(&right)}
	ael.Insert(aeL)
	ael.Insert(aeR)
	Classify(ael, aeL, OpUnion)
	Classify(ael, aeR, OpUnion)

	if aeL.WindSelf != 1 || aeR.WindSelf != 0 {
		t.Errorf("WindSelf: L=%d R=%d want 1 0", aeL.WindSelf, aeR.WindSelf)
	}
	if !aeL.Contributing || !aeR.Contributing {
		t.Errorf("Contributing: L=%v R=%v want both true", aeL.Contributing, aeR.Contributing)
	}
}

func TestClassifyTwoOverlappingSquares(t *testing.T) {
	// Subject square at X=0..10, clip square at X=5..15. Both CCW.
	// At a scanline that crosses all four vertical edges:
	//   X=0  subject left  (Reversed → +1)
	//   X=5  clip left     (Reversed → +1)
	//   X=10 subject right (Reversed=false → -1)
	//   X=15 clip right    (Reversed=false → -1)
	sL := vert(0, Subject, true)
	cL := vert(5, Clip, true)
	sR := vert(10, Subject, false)
	cR := vert(15, Clip, false)

	ael := NewAEL()
	aeSL := &ActiveEdge{Seg: &sL, CurrX: 0, WindDx: signedContribution(&sL)}
	aeCL := &ActiveEdge{Seg: &cL, CurrX: 5, WindDx: signedContribution(&cL)}
	aeSR := &ActiveEdge{Seg: &sR, CurrX: 10, WindDx: signedContribution(&sR)}
	aeCR := &ActiveEdge{Seg: &cR, CurrX: 15, WindDx: signedContribution(&cR)}
	ael.Insert(aeSL)
	ael.Insert(aeCL)
	ael.Insert(aeSR)
	ael.Insert(aeCR)

	// Classify in left-to-right order (insertion order in this case).
	for _, ae := range []*ActiveEdge{aeSL, aeCL, aeSR, aeCR} {
		Classify(ael, ae, OpUnion)
	}

	// Expected:
	//   aeSL: WindSelf=+1 WindOther=0  contributing (outer boundary entry)
	//   aeCL: WindSelf=+1 WindOther=+1 NOT contributing (inside subject)
	//   aeSR: WindSelf=0  WindOther=+1 NOT contributing (inside clip)
	//   aeCR: WindSelf=0  WindOther=0  contributing (outer boundary exit)
	cases := []struct {
		name        string
		ae          *ActiveEdge
		wantSelf    int
		wantOther   int
		wantContrib bool
	}{
		{"aeSL", aeSL, 1, 0, true},
		{"aeCL", aeCL, 1, 1, false},
		{"aeSR", aeSR, 0, 1, false},
		{"aeCR", aeCR, 0, 0, true},
	}
	for _, c := range cases {
		if c.ae.WindSelf != c.wantSelf || c.ae.WindOther != c.wantOther || c.ae.Contributing != c.wantContrib {
			t.Errorf("%s: WindSelf=%d WindOther=%d Contributing=%v; want %d %d %v",
				c.name, c.ae.WindSelf, c.ae.WindOther, c.ae.Contributing,
				c.wantSelf, c.wantOther, c.wantContrib)
		}
	}
}

func TestClassifyOpIntersect(t *testing.T) {
	// Same configuration as above; for Intersect the inside edges contribute
	// and the outside-only edges don't.
	sL := vert(0, Subject, true)
	cL := vert(5, Clip, true)
	sR := vert(10, Subject, false)
	cR := vert(15, Clip, false)

	ael := NewAEL()
	aes := []*ActiveEdge{
		{Seg: &sL, CurrX: 0, WindDx: signedContribution(&sL)},
		{Seg: &cL, CurrX: 5, WindDx: signedContribution(&cL)},
		{Seg: &sR, CurrX: 10, WindDx: signedContribution(&sR)},
		{Seg: &cR, CurrX: 15, WindDx: signedContribution(&cR)},
	}
	for _, ae := range aes {
		ael.Insert(ae)
	}
	for _, ae := range aes {
		Classify(ael, ae, OpIntersect)
	}

	// For Intersect: contribute iff WindOther != 0.
	wantContrib := []bool{false, true, true, false}
	for i, w := range wantContrib {
		if aes[i].Contributing != w {
			t.Errorf("Intersect[%d].Contributing=%v want %v", i, aes[i].Contributing, w)
		}
	}
}

func TestClassifyOpXor(t *testing.T) {
	// For Xor every flip contributes regardless of WindOther.
	sL := vert(0, Subject, true)
	cL := vert(5, Clip, true)
	sR := vert(10, Subject, false)
	cR := vert(15, Clip, false)

	ael := NewAEL()
	aes := []*ActiveEdge{
		{Seg: &sL, CurrX: 0, WindDx: signedContribution(&sL)},
		{Seg: &cL, CurrX: 5, WindDx: signedContribution(&cL)},
		{Seg: &sR, CurrX: 10, WindDx: signedContribution(&sR)},
		{Seg: &cR, CurrX: 15, WindDx: signedContribution(&cR)},
	}
	for _, ae := range aes {
		ael.Insert(ae)
	}
	for _, ae := range aes {
		Classify(ael, ae, OpXor)
	}
	for i, ae := range aes {
		if !ae.Contributing {
			t.Errorf("Xor[%d]: not contributing — should be (every flip contributes)", i)
		}
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
		if got := signedContribution(&c.seg); got != c.want {
			t.Errorf("%s: %d want %d", c.name, got, c.want)
		}
	}
}
