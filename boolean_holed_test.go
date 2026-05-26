package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

func TestBooleanInputHoleIslandNesting(t *testing.T) {
	// A is a 10x10 square with a centered 6x6 hole (area 64). B is a 2x2 square
	// entirely inside that hole (area 4). The union's three boundary rings are
	// the square (CCW, depth 0 -> filled), the 6x6 hole (CW, depth 1 -> hole of
	// the square), and B (CCW, depth 2 -> a filled ISLAND that sits in the hole,
	// hence its own top-level ExPolygon, not a hole). assembleResult computed
	// nesting depth among outer rings only, so it saw B as directly inside the
	// square (depth 1) and wrongly demoted it to a hole, dropping the real 6x6
	// hole (Union/Xor 96 instead of 68). It now builds the containment forest
	// over ALL rings (DESIGN.md §11.9). Values are exact (axis-aligned).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(2, 2).Point(2, 8).Point(8, 8).Point(8, 2).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(4, 4).Point(6, 4).Point(6, 6).Point(4, 6).MustPolygon()}}

	runOpAreaChecks(t, 0.02, []opAreaCheck{
		{opUnion, func() (geom.MultiPolygon, error) { return Union(a, b) }, 68},
		{opIntersect, func() (geom.MultiPolygon, error) { return Intersect(a, b) }, 0},
		{opDifference, func() (geom.MultiPolygon, error) { return Difference(a, b) }, 64},
		{opXor, func() (geom.MultiPolygon, error) { return Xor(a, b) }, 68},
	})

	// The union must keep the island as a SEPARATE top-level piece, and the
	// square must keep its 6x6 hole — exactly two pieces, one holed, one not.
	u, err := Union(a, b)
	require.NoError(t, err, "union")
	require.Len(t, u, 2, "union pieces = %d, want 2 (square+hole, island)", len(u))
	holed, island := 0, 0
	for _, ex := range u {
		switch len(ex.Holes) {
		case 1:
			holed++
		case 0:
			island++
		}
	}
	require.True(t, holed == 1 && island == 1, "union pieces: holed=%d island=%d, want 1 and 1", holed, island)
}

func TestBooleanHoledInputCoincidentPlateau(t *testing.T) {
	// A is a 12x12 square with a triangular hole (3,3)-(3,9)-(9,9) whose top
	// edge is a horizontal at y=9. B is a quad whose own top edge is also a
	// horizontal at y=9 that partially overlaps the hole's top, so B's local-max
	// plateau and the hole's local-max plateau are coincident over x in [3,4].
	//
	// The hole's top plateau is split by B's vertex at (4,9) into T-junction
	// fragments and is traversed past (4,9) to its true apex at (3,9). closeBound
	// wrongly deferred B's coinciding max edge to that partner plateau (matching
	// only the current fragment's far X), but the partner passes THROUGH (4,9)
	// and closes its own subject ring at (3,9) — B's clip edge was never closed
	// and lingered hot in the AEL, where the square's top horizontal later
	// crossed it and dropped the whole upper-right region (Difference 57.96
	// instead of 125.46). plateauPartnerPending now defers only when the partner
	// truly tops out at the apex, or borders the other source there (DESIGN.md
	// §12.11). The four set identities must hold to within MC/grid tolerance.
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(9, 9).Point(3, 3).Point(3, 9).Point(7, 9).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(4, 9).Point(2, 9).Point(4, 8).Point(10, 8).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Difference must not have collapsed (the bug dropped ~68 of 125.5).
	require.GreaterOrEqual(t, r.dA, 120.0, "difference area %v collapsed (want ~%v)", r.dA, r.aA-r.iA)
}

func TestBooleanHoledInputFlatHoleTopThroughClip(t *testing.T) {
	// A is a 12x12 square with a quad hole whose TOP edge is a horizontal at y=7
	// ((3,7)-(6,7)). B is a quad fully inside the square that overlaps the hole,
	// so the hole pokes out of B on the left and B's clip edge crosses the hole's
	// flat top. The difference region rides the hole's left bound up to the hole
	// apex (3,7); because the apex is the LEFT end of the hole's top plateau, the
	// hole's right bound reaches (3,7) only after traversing that horizontal,
	// which closeBound had not yet seen — so it closed the region prematurely
	// (Case A) and the plateau, crossing B's edge, fragmented the ring and dropped
	// ~64 of the 82.3 area (Difference 18.30). plateauMaxPartnerPending now defers
	// to the geometric same-source maxima partner so doHorizontal's own close
	// pairs the two and joins the rings (DESIGN.md §12.11). Tilting the hole top
	// off-horizontal already worked; this asserts the flat-top variant matches.
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(8, 4).Point(5, 5).Point(3, 7).Point(6, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(11, 4).Point(7, 12).Point(0, 1).Point(4, 0).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Difference must not have collapsed (the bug dropped ~64 of ~82.3).
	require.GreaterOrEqual(t, r.dA, 78.0, "difference area %v collapsed (want ~%v)", r.dA, r.aA-r.iA)
}

func TestBooleanHoledInputDifferenceClipApexSameSideJoin(t *testing.T) {
	// A is a 12x12 square with a hole [[6,6],[6,9],[9,9],[7,3]] (vertical left edge,
	// horizontal top at y=9). B [[5,12],[4,0],[9,10],[5,8]] is a non-convex clip with
	// a notch peak at (9,10). In Difference, two output regions outside B's two edges
	// meet at the clip apex (9,10) arriving BOTH-BACK; AddLocalMaxPoly's same-side
	// figure-8 splice only handled the both-FRONT case, so the both-back join fell to
	// the relabel+JoinOutrecPaths path, which reversed a sub-chain and emitted a
	// GEOMETRICALLY self-crossing ring (no repeated vertex, so splitSelfTouchingRings
	// could not fix it) whose figure-8 shoelace under-counted — Difference collapsed
	// to 94.35 vs the correct ~122.07. The new both-BACK figure-8 mirror in
	// AddLocalMaxPoly splices it as a clean self-touching detour (DESIGN.md §12.11).
	//
	// NOTE: a separate, smaller residual (~1.5) remains in B's notch region near
	// (7,9)/(9,10) (tracked in memory as the next target); this test guards the
	// catastrophic-collapse fix via a lower bound, not the exact identity.
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(6, 6).Point(6, 9).Point(9, 9).Point(7, 3).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(5, 12).Point(4, 0).Point(9, 10).Point(5, 8).MustPolygon()}}
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	want := a.Area() - i.Area() // D = A - I, ~122.07
	// The catastrophic self-crossing collapse dropped ~28 (got 94.35). Assert the
	// tangle is gone: D must be within ~2 of the identity (was off by ~28).
	require.InDelta(t, want, d.Area(), 2.0, "difference area %v collapsed (want ~%v, tolerance documents the small notch residual)", d.Area(), want)
}

func TestBooleanHoledInputHoleTopCoincidentWithClipContinuingEdge(t *testing.T) {
	// A is a 12x12 square with a triangular hole (input [[5,4],[3,3],[5,5],[8,5]]
	// simplifies to (3,3),(5,5),(8,5)) whose flat TOP (5,5)-(8,5) is COINCIDENT
	// with a CONTINUING edge of B: B's vertex (5,5) equals the hole apex, and B's
	// bottom edge (5,5)->(12,5) (split to (5,5)->(8,5) at the hole's (8,5)) runs
	// along the same horizontal while B's bound carries on UP to its apex (5,10).
	// At the (5,5) confluence the Intersect region's ring correctly hands off onto
	// B's continuing right bound, but the coincident pair (B's hot continuing
	// horizontal vs the hole's cold dead-end top) has EQUAL Reversed flags, so the
	// opposite-side skip missed it and the one-hot SwapOutrecs transferred the
	// region ring onto the cold dead-end hole-top — collapsing it to a degenerate
	// 2-pt ring, so Intersect returned 0 instead of ~21 (and U/D/X identities all
	// broke off that I=0). sameSideHotContinuesColdEnds now also skips a same-side
	// coincident pair when the hot bound passes THROUGH the overlap (apex strictly
	// above) while the cold bound ends there (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 4).Point(3, 3).Point(5, 5).Point(8, 5).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(5, 5).Point(12, 5).Point(5, 10).Point(3.5, 3.25).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Intersect must not have collapsed (the bug returned 0 instead of ~21).
	require.GreaterOrEqual(t, r.iA, 18.0, "intersect area %v collapsed (want ~21)", r.iA)
}

func TestBooleanHoledInputDifferenceCoincidentBothHotExit(t *testing.T) {
	// Difference where the subject hole's TOP edge (4,8)-(7,8) is coincident with
	// the clip's BOTTOM edge (4,8)-(7,8) — a doubled boundary — and BOTH edges are
	// hot at the overlap (one ring from the cross-source crossing below, one from
	// the clip's local min). The clip bound CONTINUES up past the overlap while the
	// hole-top is bound-last (ends). The dispatchIntersect coincident skip is
	// designed for hot+cold pairs, so this both-hot pair fell to branchBothHot,
	// which AddLocalMaxPoly-merged the two rings through their coincident edge into
	// a phantom sliver → D returned 141.6 (with a spurious island) instead of
	// ~130.5. sameSideBothHotOneEnds now also skips a both-hot one-continues/one-ends
	// coincident pair (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(4, 7).Point(4, 8).Point(7, 8).Point(3, 6).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(4, 8).Point(7, 8).Point(2, 10).Point(2, 2).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	require.InDelta(t, aA-iA, dA, 0.02, "D=A-I: got %v want %v", dA, aA-iA)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{identU, uA, aA + bA - iA},
		{identX, xA, uA - iA},
	} {
		require.InDelta(t, c.want, c.got, 0.02, "%s: got %v want %v", c.name, c.got, c.want)
	}
}

func TestBooleanHoledInputIntersectClipApexThroughHole(t *testing.T) {
	// Intersect where the clip's apex coincides with a point a subject hole edge
	// passes through. The intersection ring rides the hole hypotenuse, and at the
	// clip apex (5.5,5) — where both clip top edges close, one via an unswept
	// horizontal — the hypotenuse's clip-winding must drop so the ring closes there.
	// closeBound used to close only the clip edge while the hypotenuse continued up
	// into the hole interior, emitting the hole's upper triangle as a spurious
	// filled ring (I 15.4 vs ~10.4). plateauMaxPartnerPending now defers the clip
	// apex when its cross-source coupled through-edge passes through (not onto a
	// shared horizontal), so doHorizontal's plateau closes it correctly. The deg1
	// sibling below (two quads sharing a top edge) must NOT defer and stays correct.
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(3, 3).Point(3, 8).Point(3, 7).Point(8, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(5.5, 5).Point(2, 5).Point(8, 1).Point(10, 2).MustPolygon()}}
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	u, _ := Union(a, b)
	d, _ := Difference(a, b)
	x, _ := Xor(a, b)
	aA, bA := a.Area(), b.Area()
	iA := i.Area()
	require.LessOrEqual(t, iA, 11.5, "intersect %v over-counted (want ~10.4) — spurious hole-interior ring", iA)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{identU, u.Area(), aA + bA - iA},
		{identD, d.Area(), aA - iA},
		{identX, x.Area(), u.Area() - iA},
	} {
		require.InDelta(t, c.want, c.got, 0.02, "%s: got %v want %v", c.name, c.got, c.want)
	}

	// Sibling that must NOT trigger the cross-source defer: two quads sharing a top
	// edge (no holes). Difference must stay correct (the defer here would drop area).
	a2 := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(4, 2).Point(12, 8).Point(8, 8).Point(6, 8).MustPolygon()}}
	b2 := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(8, 8).Point(5, 11).Point(1, 1).Point(12, 8).MustPolygon()}}
	d2, err := Difference(a2, b2)
	require.NoError(t, err, "difference")
	i2, _ := Intersect(a2, b2)
	require.InDelta(t, a2.Area()-i2.Area(), d2.Area(), 0.02, "shared-top D=A-I: got %v want %v", d2.Area(), a2.Area()-i2.Area())
}

func TestBooleanHoledInputIntersectHoleNotchPlateauDefer(t *testing.T) {
	// Intersect where a clip quad pokes into a subject hole and the hole's top is a
	// horizontal plateau. The intersection ring rides the hole's left bound up to
	// the hole apex while COUPLED to the clip edge it crossed (cross-source ring);
	// at the apex the ring must continue along the hole's top plateau, but that
	// plateau hasn't been swept yet, so closeBound closed eagerly and a stray
	// AddLocalMinPoly spawned a phantom interior hole → Intersect 7.3 instead of
	// ~15.5. plateauMaxPartnerPending now also defers a CROSS-source-coupled apex
	// when the coupled edge is sloped, continues above, and is off the apex column
	// (DESIGN.md §12.11); the coincident-plateau confluence (coupled edge horizontal
	// / topping out / on the apex column) is excluded so it still closes normally.
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(9, 7).Point(7, 6).Point(5, 5).Point(4, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(7, 8).Point(2, 7).Point(0, 1).Point(10, 10).MustPolygon()}}

	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	r := runBooleanIdentities(t, a, b, 0.02)
	require.GreaterOrEqual(t, r.iA, 14.0, "intersect area %v collapsed (want ~15.3)", r.iA)
	// No phantom interior hole: the intersection is a single simple region.
	require.True(t, len(i) == 1 && len(i[0].Holes) == 0, "intersect should be one hole-free ring, got %d pieces %v", len(i), i)
}

func TestBooleanHoledInputIntersectHoleExitReheat(t *testing.T) {
	// Intersect where a clip quad's boundary crosses a subject hole that pokes
	// back OUT through the clip (a notch). The intersection ring rides the hole's
	// left bound up to the hole apex, where it must continue onto the clip's TOP
	// edge — but that clip bound went cold at the cross-source crossing and only
	// reaches the apex later this scanline, by traversing its coincident top
	// horizontal in doHorizontal. Closing the hole bound's apex eagerly dropped
	// the region past the hole (Intersect collapsed to a sliver, ~4.7 vs ~15.5).
	// closeBound now DEFERS such a hot apex when a cold cross-source bound will
	// traverse a horizontal through it (crossSourceHorizThroughPending), so the
	// horizontal's crossing re-heats that bound onto the ring (DESIGN.md §12.11).
	cases := []struct {
		name string
		hole geom.Polygon
		b    geom.Polygon
		want float64
	}{
		{"poke-up", geom.New().Point(9, 9).Point(5, 6).Point(5, 7).Point(4, 9).MustPolygon(), geom.New().Point(0, 11).Point(9, 0).Point(6, 9).Point(2, 9).MustPolygon(), 15.456},
		{"poke-down", geom.New().Point(3, 3).Point(6, 6).Point(8, 6).Point(8, 4).MustPolygon(), geom.New().Point(0, 10).Point(6, 0).Point(7.5, 6).Point(6, 6).MustPolygon(), 16.92},
	}
	outer := geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon()
	for _, tc := range cases {
		a := geom.MultiPolygon{geom.ExPolygon{Outer: outer, Holes: []geom.Polygon{tc.hole}}}
		b := geom.MultiPolygon{geom.ExPolygon{Outer: tc.b}}
		u, err := Union(a, b)
		require.NoError(t, err, "%s union", tc.name)
		i, err := Intersect(a, b)
		require.NoError(t, err, "%s intersect", tc.name)
		d, err := Difference(a, b)
		require.NoError(t, err, "%s difference", tc.name)
		x, err := Xor(a, b)
		require.NoError(t, err, "%s xor", tc.name)
		aA, bA := a.Area(), b.Area()
		uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
		// Intersect must not have collapsed to a sliver (the bug returned ~5).
		require.GreaterOrEqual(t, iA, tc.want-0.6, "%s: intersect area %v collapsed (want ~%v)", tc.name, iA, tc.want)
		for _, c := range []struct {
			name      string
			got, want float64
		}{
			{identU, uA, aA + bA - iA},
			{identD, dA, aA - iA},
			{identX, xA, uA - iA},
		} {
			require.InDelta(t, c.want, c.got, 0.02, "%s %s: got %v want %v", tc.name, c.name, c.got, c.want)
		}
	}
}

func TestBooleanHoledInputDifferenceHoleClipVoidMerge(t *testing.T) {
	// Difference where a subject hole and the clip overlap so their VOIDS merge.
	// A is a 12x12 square with hole [[8,8],[6,8],[4,3],[3,8]] (a triangle with a
	// flat top at y=8); B's bottom edge (3,8)-(5,8) is coincident with that top and
	// B pokes up out of the square's interior. The correct result is the square
	// with ONE hole = hole∪B (~25.9 void), area ~118.1. The hole's right bound and
	// B's left bound cross where the filled strip between the two voids closes; the
	// AddLocalMaxPoly join correctly merges the two void rings, but the surviving
	// ring's other bound (B's left, cross-source coupled) tops out at the square
	// corner with a COLD same-source maxima partner (B's top went cold at the
	// crossing). The old code removed it without emitting the apex, splicing a
	// phantom edge that collapsed the void to a 5.4 sliver → D returned 138.6
	// instead of ~118.1. closeBound now closes such a hot edge via its coupled edge
	// (Case A/B) instead of dropping it (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(8, 8).Point(6, 8).Point(4, 3).Point(3, 8).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(3, 8).Point(5, 8).Point(12, 6).Point(10, 11).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()

	// Difference must not have collapsed to a sliver (the bug returned 138.6,
	// larger than A's area, instead of ~118.1).
	require.LessOrEqual(t, dA, aA-10, "difference area %v did not remove the hole-clip void (want ~118.1, A=%v)", dA, aA)
	require.InDelta(t, aA-iA, dA, 0.02, "D=A-I: got %v want %v", dA, aA-iA)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{identU, uA, aA + bA - iA},
		{identX, xA, uA - iA},
	} {
		require.InDelta(t, c.want, c.got, 0.02, "%s: got %v want %v", c.name, c.got, c.want)
	}
}

func TestBooleanHoledInputDifferenceHoleTopPlateauVoidMerge(t *testing.T) {
	// Difference where a subject hole's left bound is made HOT by a bite crossing
	// (the clip's void ring rides onto it) and rises to the hole apex (5,9); the
	// hole's trailing TOP horizontal (5,9)-(9,9) is its cold same-source maxima
	// partner. The void boundary must continue from the apex along that horizontal
	// to (9,9), where the clip's right bound re-bounds the merged void. The old
	// maximaPartner remove-both dropped the hot partner without emitting its apex or
	// tracing the horizontal, leaving the hole's uncovered apex region SOLID — D
	// returned 116 instead of ~110.46 (under-removed by 5.54, the part of the hole
	// not covered by B). differenceNotchPlateauJoin now joins the hot partner's ring
	// to the clip ring at the horizontal's near end (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 9).Point(9, 9).Point(9, 6).Point(9, 3).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(10, 0).Point(9, 9).Point(2, 0).Point(5, 2).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()

	// The hole's uncovered apex region must be removed (the bug left D at 116).
	require.LessOrEqual(t, dA, 112.0, "difference area %v did not remove the hole apex region (want ~110.46)", dA)
	for _, c := range []struct {
		name      string
		got, want float64
	}{
		{identD, dA, aA - iA},
		{identU, uA, aA + bA - iA},
		{identX, xA, uA - iA},
	} {
		require.InDelta(t, c.want, c.got, 0.02, "%s: got %v want %v", c.name, c.got, c.want)
	}
}

func TestBooleanHoledInputXorHoleClipApexFigure8(t *testing.T) {
	// Xor where a subject hole and the clip share an edge so their boundaries meet
	// same-side BOTH-BACK at the clip's apex. polyclip's mirrored front/back makes
	// the two input-min rings arrive same-side here; the figure-8 PINCH then
	// double-counts the hole∩clip overlap lens, so splitSelfTouchingRings emits
	// three overlapping holes and X under-counted (got 125.5 vs ~127.3 for the
	// first input below; 114.5 vs 118.5 for the second). AddLocalMaxPoly now
	// detects this mirror artifact (same source, both-back, equal other-winding,
	// BOTH rings spawned at input minima — not a crossing) and reverses one ring to
	// join opposite-side instead of pinching (DESIGN.md §12.11).
	cases := []struct {
		name string
		hole geom.Polygon
		b    geom.Polygon
	}{
		{"shared-edge", geom.New().Point(5, 9).Point(8, 9).Point(6, 4).Point(5, 4).MustPolygon(),
			geom.New().Point(8, 9).Point(6, 4).Point(10, 12).Point(3, 8).MustPolygon()},
		{"shared-vertex", geom.New().Point(7, 8).Point(9, 3).Point(3, 4).Point(6, 8).MustPolygon(),
			geom.New().Point(1, 2).Point(7, 8).Point(10, 2).Point(6, 11).MustPolygon()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := geom.MultiPolygon{geom.ExPolygon{
				Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
				Holes: []geom.Polygon{tc.hole},
			}}
			b := geom.MultiPolygon{geom.ExPolygon{Outer: tc.b}}
			u, err := Union(a, b)
			require.NoError(t, err, "union")
			i, err := Intersect(a, b)
			require.NoError(t, err, "intersect")
			d, err := Difference(a, b)
			require.NoError(t, err, "difference")
			x, err := Xor(a, b)
			require.NoError(t, err, "xor")
			aA, bA := a.Area(), b.Area()
			uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
			for _, c := range []struct {
				name      string
				got, want float64
			}{
				{identX, xA, uA - iA},
				{identU, uA, aA + bA - iA},
				{identD, dA, aA - iA},
			} {
				require.InDelta(t, c.want, c.got, 0.02, "%s: got %v want %v", c.name, c.got, c.want)
			}
		})
	}
}

func TestBooleanHoledInputHoleNotchApexReconnection(t *testing.T) {
	// Intersect where a clip quad's left bound crosses INTO a triangular subject
	// hole, biting a notch out of the clip. A is a 12x12 square with hole
	// [[7,7],[5,4],[5,7],[3,7]] (simplifies to triangle (7,7),(5,4),(5,7), apex at
	// the plateau (5,7)-(7,7)). B = [[3,7],[5,7],[11,1],[7,11]] shares vertex (5,7).
	// B builds the Intersect ring; at the crossing (6.2,5.8) the ring correctly
	// turns from B's left bound onto the hole's right edge (into the notch) and
	// rides it up to the hole apex (5,7). There it must hand off onto B's left
	// continuation (which went cold at the crossing and already traversed its
	// (5,7)->(3,7) horizontal). Without the apexNotchContinuation handoff the ring
	// collapsed to a tiny sliver, so Intersect returned ~1.2 instead of ~20.85 and
	// all three U/D/X identities broke (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(7, 7).Point(5, 4).Point(5, 7).Point(3, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(3, 7).Point(5, 7).Point(11, 1).Point(7, 11).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Intersect must not have collapsed (the bug returned ~1.2 instead of ~20.8).
	require.GreaterOrEqual(t, r.iA, 19.0, "intersect area %v collapsed (want ~20.8)", r.iA)
}

func TestBooleanHoledInputHoleTopDeadEndsOnClipThroughVertex(t *testing.T) {
	// The non-coincident sibling of …HoleTopCoincidentWithSlopedClipBound: by the
	// time the two boundaries meet, the hot clip bound has ALREADY climbed off the
	// coincident horizontal onto its sloped continuation, so the meeting is a plain
	// one-hot crossing rather than a coincident-horizontal pair. A is a 12x12
	// square with hole [[5,5],[3,9],[5,9],[9,9]] whose top is horizontal at y=9.
	// B = [[3,9],[5,9],[0,11],[2,3]] shares the hole-top sub-edge (3,9)-(5,9), then
	// B climbs (5,9)->(0,11). At (5,9) the hole's cold dead-end top horizontal
	// S(3,9)->(5,9) crosses B's hot through-edge; the one-hot SwapOutrecs used to
	// transfer the live Intersect ring onto that cold dead-end, collapsing Intersect
	// to 0 (want ~12, and U/D/X identities broke off that I=0). coldDeadEndAtHotThrough
	// now suppresses the transfer when the cold edge is a bound-last horizontal
	// dead-ending on the hot bound that just climbed off a coincident horizontal
	// (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 5).Point(3, 9).Point(5, 9).Point(9, 9).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(3, 9).Point(5, 9).Point(0, 11).Point(2, 3).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Intersect must not have collapsed (the bug returned 0 instead of ~12).
	require.GreaterOrEqual(t, r.iA, 10.0, "intersect area %v collapsed (want ~12)", r.iA)
}

func TestBooleanHoledInputHoleTopCoincidentWithSlopedClipBound(t *testing.T) {
	// Sibling of …HoleTopCoincidentWithClipContinuingEdge, but here the hot
	// clip bound continues past the coincident overlap with a SLOPED edge, not
	// another collinear horizontal. A is a 12x12 square with hole
	// [[3,9],[5,8],[7,8],[7,7]] whose top edge (5,8)-(7,8) is horizontal at y=8.
	// B = [[5,8],[7,8],[0,12],[0,6]] shares that exact edge: B's bottom edge
	// (5,8)-(7,8) is coincident with the hole top (same Reversed), then B's bound
	// climbs (7,8)->(0,12). At the (7,8) confluence the one-hot SwapOutrecs used
	// to transfer the live Intersect ring onto the hole's cold dead-end top,
	// collapsing Intersect to 0 (want ~19, and U/D/X identities broke off that
	// I=0). sameSideHotContinuesColdEnds now skips the coincident pair whenever
	// the hot bound passes THROUGH the overlap (apex strictly above) regardless of
	// whether its continuation is horizontal or sloped (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(3, 9).Point(5, 8).Point(7, 8).Point(7, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(5, 8).Point(7, 8).Point(0, 12).Point(0, 6).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Intersect must not have collapsed (the bug returned 0 instead of ~19).
	require.GreaterOrEqual(t, r.iA, 17.0, "intersect area %v collapsed (want ~19)", r.iA)
}

func TestBooleanHoledInputHoleTopCoincidentWithClipTop(t *testing.T) {
	// A is a 12x12 square with a triangular hole whose TOP edge is a horizontal
	// at y=9 ((5,9)-(9,9)); the input hole [[5,4],[5,9],[9,9],[5,7]] has a
	// degenerate zero-width spike along x=5 that simplifyCollinearRing strips to
	// that triangle. B is a quad fully inside the square whose own TOP edge is
	// also a horizontal at y=9 ((1,9)-(10,9)) — coincident with the hole's top
	// over x[5,9], and the hole sits entirely inside B. The coincident pair is an
	// opposite-side (Reversed-differing) doubled boundary, so dispatchIntersect
	// should SKIP it; but B's top continues collinearly past the overlap to
	// (10,9), and the old continuesCollinearHorizontal guard blocked the skip even
	// though B's continuing bound was already HOT — the one-hot SwapOutrecs then
	// transferred B's ring onto the cold dead-end hole edge, emitting B's region
	// as a stray positive ring and the square as a full ring with no hole
	// (Difference 152 instead of 116). collinearContinuationBlocksSkip now blocks
	// the skip only when the continuing bound is COLD (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 4).Point(5, 9).Point(9, 9).Point(5, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(1, 1).Point(10, 9).Point(1, 9).Point(3, 5).MustPolygon()}}

	r := runBooleanIdentities(t, a, b, 0.02)

	// Difference must not have over-counted (the bug emitted 152 > A.Area).
	require.LessOrEqual(t, r.dA, r.aA+0.02, "difference area %v over-counts (want ~%v)", r.dA, r.aA-r.iA)
}

func TestBooleanDifferenceIdenticalRotatedCancels(t *testing.T) {
	// A and B are the SAME quad with vertices rotated by one position, so the
	// mpolyEqual idempotency short-circuit (which compares vertex order) does
	// NOT fire and the engine runs. The sweep emits the region twice — once CCW
	// and once CW (coincident boundaries) — which must cancel to zero area.
	// assembleResult's containment forest treats two equal-area coincident
	// rings as outer+hole via an orientation tie-break (DESIGN.md §11.9); a
	// strict larger-area rule alone left both as filled outers (area doubled).
	a := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(7, 11).Point(7, 8).Point(5, 3).Point(12, 2).MustPolygon()}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(7, 8).Point(5, 3).Point(12, 2).Point(7, 11).MustPolygon()}}
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	require.LessOrEqual(t, d.Area(), 0.02, "Difference area %v want 0", d.Area())
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	require.LessOrEqual(t, x.Area(), 0.02, "Xor area %v want 0", x.Area())
}

func TestBooleanHoledInputDifferenceClipApexAtHoleVertex(t *testing.T) {
	// The clip apex (8,8) coincides with a subject hole vertex (8,8). Resolving
	// the clip-apex maximum, resolveBetweenMaxima crosses the cold clip edge with
	// the cold hole-right edge (both converge at (8,8)), spawning a ring whose
	// front/back is mis-oriented because the mid-resolution AEL is transient. The
	// two apex edges then arrive SAME-side (both back), forcing the figure-8
	// workaround, which merged the real void into the spurious spawn and emitted a
	// degenerate spur (5,8)(8,8)(8,8) that splitSelfTouchingRings drops → D
	// returned 128.9 instead of ~119.9. AddLocalMaxPoly now reverses the
	// continuing (spawned) ring's sides so the pair is opposite-side and splices
	// via the standard JoinOutrecPaths (DESIGN.md §12.11, clip-apex/hole-vertex).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 8).Point(8, 8).Point(6, 4).Point(6, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(0, 7).Point(1, 7).Point(12, 2).Point(8, 8).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	require.InDelta(t, aA-iA, dA, 0.02, "%s: got %v want %v", identD, dA, aA-iA)
	require.InDelta(t, aA+bA-iA, uA, 0.02, "%s: got %v want %v", identU, uA, aA+bA-iA)
	require.InDelta(t, uA-iA, xA, 0.02, "%s: got %v want %v", identX, xA, uA-iA)
}

func TestBooleanHoledInputUnionHoleTopCoincidentWithFillingClip(t *testing.T) {
	// A subject hole's TOP edge (3,7)-(8,7) is coincident-collinear with the TOP
	// edge of a clip B that fills the hole from below (B's apex (8,7) and edge
	// (8,7)-(3,7) lie on the hole top). In Union/Xor the hole shrinks to B's
	// boundary, so the coincident hole top is an interior doubled boundary that
	// must cancel. polyclip's incremental WindOther never counted B for the hole
	// top (the hole-right and B-right converge at the shared apex (8,7) without
	// crossing), so the hole top wrongly stayed contributing and traced a phantom
	// span — Union returned 137.5 (hole left too big) instead of ~141.35.
	// closeBound now folds the terminating clip edge's winding into its coupled
	// hole bound at the shared apex and closes the ring when that makes the hole
	// top non-contributing (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(9, 6).Point(7, 5).Point(3, 7).Point(8, 7).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(3, 3).Point(8, 7).Point(3, 7).Point(3, 1).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	require.InDelta(t, aA+bA-iA, uA, 0.02, "%s: got %v want %v", identU, uA, aA+bA-iA)
	require.InDelta(t, aA-iA, dA, 0.02, "%s: got %v want %v", identD, dA, aA-iA)
	require.InDelta(t, uA-iA, xA, 0.02, "%s: got %v want %v", identX, xA, uA-iA)
	require.InDelta(t, 141.346, uA, 0.02, "union area: got %v want ~141.346", uA)
}

func TestBooleanHoledInputUnionHoleTopCoincidentMaxPlateau(t *testing.T) {
	// A subject hole's TOP plateau (5,9)-(9,9) overlaps the TOP max-plateau of a
	// clip B that fills the hole from below: B = triangle (0,2),(5,9),(8,9) whose
	// top edge (5,9)-(8,9) is coincident with the hole top's left portion. Both
	// tops are local-MAX plateaus (not bound continuations), so the earlier
	// shared-apex winding fold does NOT fire. The hole-top piece (5,9)-(8,9) is an
	// interior doubled boundary (solid A above, B fill below), but polyclip traced
	// it — the void ring detoured to (5,9) and Union returned 126.167 (hole left
	// too big) instead of ~130.902. closeBound's Case-B close now suppresses the
	// interior maxPt when ae's trailing horizontal coincides with a cold
	// cross-source max-plateau and its coupled cross-source edge tops at ae's near
	// endpoint (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(5, 9).Point(9, 9).Point(9, 4).Point(4, 6).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(6, 9).Point(5, 9).Point(0, 2).Point(8, 9).MustPolygon()}}

	u, err := Union(a, b)
	require.NoError(t, err, "union")
	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	require.InDelta(t, aA+bA-iA, uA, 0.02, "%s: got %v want %v", identU, uA, aA+bA-iA)
	require.InDelta(t, aA-iA, dA, 0.02, "%s: got %v want %v", identD, dA, aA-iA)
	require.InDelta(t, uA-iA, xA, 0.02, "%s: got %v want %v", identX, xA, uA-iA)
	require.InDelta(t, 130.902, uA, 0.02, "union area: got %v want ~130.902", uA)
}

func TestBooleanHoledInputIntersectHoleBiteThroughApex(t *testing.T) {
	// B = quad (1,2),(7,8),(10,2),(6,11) lies inside the square but its edge
	// (1,2)-(7,8) passes through subject hole vertex (3,4), so the hole bites a
	// triangle (3,4),(6,8),(7,8) (area 2) out of B. In Intersect the bite must be
	// carved: the intersection ring rides the hole-left bound up to the hole's
	// max-plateau apex (6,8), traces the plateau to the hole apex (7,8), and
	// rejoins the clip ring there. polyclip closed the ring prematurely at (6,8)
	// (the hole's max-plateau being cold), so Intersect returned 13.5 (all of B)
	// instead of 11.5, breaking the U/D identities. intersectNotchPlateau now joins
	// the notch to the continuing clip ring at the hole apex (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(7, 8).Point(9, 3).Point(3, 4).Point(6, 8).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(1, 2).Point(7, 8).Point(10, 2).Point(6, 11).MustPolygon()}}

	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	u, err := Union(a, b)
	require.NoError(t, err, "union")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	aA := a.Area()
	bA := b.Area()
	iA := i.Area()
	require.InDelta(t, 11.5, iA, 0.02, "intersect area: got %v want ~11.5", iA)
	require.InDelta(t, aA+bA-iA, u.Area(), 0.02, "%s: got %v want %v", identU, u.Area(), aA+bA-iA)
	require.InDelta(t, aA-iA, d.Area(), 0.02, "%s: got %v want %v", identD, d.Area(), aA-iA)
}

func TestBooleanHoledInputIntersectHoleTopCoincidentClipTop(t *testing.T) {
	// Subject hole top is the y=6 plateau (3,6)-(6,6)-(9,6); clip B shares vertices
	// (3,6),(6,6) and its mid-bound top edge (3,6)-(6,6) is COINCIDENT with the
	// hole top's left piece, with B's left bound continuing RIGHTWARD up past (6,6)
	// to (8,11). In Intersect the Intersect ring rode the bite onto the hole-left,
	// topped at the shared vertex (3,6) coincident with B-left's apex, and B-left's
	// continuation traced the coincident interior B-top (3,6)-(6,6), tangling into a
	// self-touching ring that shattered into slivers — Intersect returned 0.305
	// instead of ~1.733. closeBound's coincident-collinear cancellation now also
	// fires for a RIGHTWARD coupled continuation when that horizontal is coincident
	// with an edge of ae's own source (a doubled interior boundary); the ring closes
	// at the shared vertex and B-left goes cold (DESIGN.md §12.11).
	a := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(12, 0).Point(12, 12).Point(0, 12).MustPolygon(),
		Holes: []geom.Polygon{geom.New().Point(9, 6).Point(8, 3).Point(3, 6).Point(6, 6).MustPolygon()},
	}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().Point(3, 6).Point(6, 4).Point(8, 11).Point(6, 6).MustPolygon()}}

	i, err := Intersect(a, b)
	require.NoError(t, err, "intersect")
	u, err := Union(a, b)
	require.NoError(t, err, "union")
	d, err := Difference(a, b)
	require.NoError(t, err, "difference")
	x, err := Xor(a, b)
	require.NoError(t, err, "xor")
	aA, bA := a.Area(), b.Area()
	iA := i.Area()
	require.InDelta(t, 1.733, iA, 0.02, "intersect area: got %v want ~1.733", iA)
	require.InDelta(t, aA+bA-iA, u.Area(), 0.02, "%s: got %v want %v", identU, u.Area(), aA+bA-iA)
	require.InDelta(t, aA-iA, d.Area(), 0.02, "%s: got %v want %v", identD, d.Area(), aA-iA)
	require.InDelta(t, u.Area()-iA, x.Area(), 0.02, "%s: got %v want %v", identX, x.Area(), u.Area()-iA)
}
