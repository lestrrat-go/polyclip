package polyclip_test

import (
	"math"
	"testing"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// TestMidBoundHorizontalRepro is a captured makislicer slice operand pair that
// used to make Difference return ErrHorizontalNotSupported: the operands are
// non-simple (a polygon and its hole snapped onto a shared vertex, plus sliver
// spikes), which broke BuildLocalMinima's ring reconstruction and dropped the
// engine onto the legacy fallback that cannot handle the staircase horizontal.
// BuildLocalMinima now decomposes such self-touching rings, so the bound model
// handles the input directly. The result is verified against the algebraic
// set-identity oracle (the same idD/idX checks the differential harness uses),
// not merely "no error".
func TestMidBoundHorizontalRepro(t *testing.T) {
	a := geom.MultiPolygon{
		{Outer: geom.Polygon{{X: 145.6520606076065, Y: 135.15999999999988}, {X: 110.34793625673223, Y: 135.1599999999999}, {X: 110.1871594847569, Y: 134.99922009236352}, {X: 108.84000000000009, Y: 133.65206060760673}, {X: 108.84000000000009, Y: 108.3479393923935}, {X: 110.3479393923935, Y: 106.84000000000009}, {X: 145.6520606076065, Y: 106.84000000000009}, {X: 147.1599999999999, Y: 108.34793939239349}, {X: 147.1599999999999, Y: 133.6520606076065}}, Holes: []geom.Polygon{{{X: 135.40999999999988, Y: 126.01355377197255}, {X: 135.40999999999988, Y: 115.98644622802746}, {X: 120.5900000000001, Y: 115.98644622802745}, {X: 120.5900000000001, Y: 126.01355377197255}}}},
		{Outer: geom.Polygon{{X: 1599.0789867131036, Y: 1624.720984557724}, {X: 108.28380114594606, Y: 134.28373300641485}, {X: 108.8400000000001, Y: 134.0533087787259}, {X: 108.8400000000001, Y: 134.2461107381011}}},
	}
	b := geom.MultiPolygon{
		{Outer: geom.Polygon{{X: 109.0646668884234, Y: 133.87672749602996}, {X: 108.84000000000015, Y: 133.6520606076067}, {X: 108.95467957555525, Y: 133.76674018316157}}},
		{Outer: geom.Polygon{{X: 135.41000000000008, Y: 126.81355377197269}, {X: 120.58999999999992, Y: 126.81355377197269}, {X: 120.58999999999992, Y: 126.21355377197267}, {X: 120.58999999999992, Y: 115.18644622802731}, {X: 135.41000000000008, Y: 115.18644622802731}}, Holes: []geom.Polygon{{{X: 120.59000000000015, Y: 115.98644622802749}, {X: 120.59000000000015, Y: 126.01355377197251}, {X: 120.59000000000015, Y: 126.21355377197267}, {X: 120.58999999999992, Y: 126.21355377197267}, {X: 120.59000000000015, Y: 126.4135537719726}, {X: 135.40999999999985, Y: 126.4135537719726}, {X: 135.40999999999985, Y: 126.01355377197251}, {X: 135.40999999999985, Y: 115.98644622802749}, {X: 135.40999999999985, Y: 115.78644622802744}, {X: 120.59000000000015, Y: 115.78644622802744}}}},
		{Outer: geom.Polygon{{X: 1599.0789867131036, Y: 1624.720984557724}, {X: 108.28380114594609, Y: 134.28373300641488}, {X: 108.83999999999992, Y: 134.05330877872598}, {X: 108.84000000000015, Y: 134.05330877872586}, {X: 108.84000000000015, Y: 134.24611073810104}, {X: 108.84000000000015, Y: 134.24611073810115}, {X: 379.9352424586184, Y: 405.384264349162}, {X: 1599.0789791313018, Y: 1624.7209769747226}, {X: 1599.0789791328734, Y: 1624.7209769762937}}, Holes: []geom.Polygon{{{X: 1599.0787033727784, Y: 1624.7207012744716}, {X: 108.83999999999992, Y: 134.24611083690684}, {X: 108.83999999999992, Y: 134.05330877872632}, {X: 108.43004019231887, Y: 134.22314852467287}, {X: 108.28380114594643, Y: 134.28373300641488}}}},
		{Outer: geom.Polygon{{X: 110.34793635146661, Y: 135.15999999999985}, {X: 110.34793625673228, Y: 135.15999999999985}, {X: 110.18715948475688, Y: 134.99922009236354}, {X: 110.17744455068657, Y: 134.98950515829324}, {X: 110.1774445448765, Y: 134.98950515248316}, {X: 110.16116563563821, Y: 134.97322624324488}, {X: 109.41108791706631, Y: 134.22314852467287}, {X: 110.19201695469701, Y: 135.00407756230356}}, Holes: []geom.Polygon{{{X: 110.19444568966719, Y: 135.00650629727386}, {X: 110.19201696051721, Y: 135.00407756812388}, {X: 110.34793635146661, Y: 135.15999999999985}, {X: 110.34793639883378, Y: 135.15999999999985}}}},
	}
	area := func(m geom.MultiPolygon, err error) float64 {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		return m.Area()
	}

	// Engine's own non-zero-winding interpretation of each (self-intersecting)
	// operand, via Simplify — also exercises the same BuildLocalMinima path.
	areaA := area(polyclip.Simplify(a))
	areaB := area(polyclip.Simplify(b))
	u := area(polyclip.Union(a, b))
	i := area(polyclip.Intersect(a, b))
	d := area(polyclip.Difference(a, b))
	x := area(polyclip.Xor(a, b))

	// Set identities that must hold for any internally-consistent boolean
	// engine, independent of the winding convention used on the input:
	//   U = A + B - I,  D = A - I,  X = U - I.
	// A non-trivial result (the main ~40x40 region survives) plus these
	// identities rules out the slicer's old failure mode (treating the op as a
	// no-op, producing a fully-solid layer).
	const tol = 1e-3
	if d < 1.0 {
		t.Fatalf("Difference area %.6f is degenerate; expected the main region to survive", d)
	}
	if got, want := u, areaA+areaB-i; math.Abs(got-want) > tol {
		t.Errorf("Union identity: U=%.6f want A+B-I=%.6f (diff %.2e)", got, want, math.Abs(got-want))
	}
	if got, want := d, areaA-i; math.Abs(got-want) > tol {
		t.Errorf("Difference identity: D=%.6f want A-I=%.6f (diff %.2e)", got, want, math.Abs(got-want))
	}
	if got, want := x, u-i; math.Abs(got-want) > tol {
		t.Errorf("Xor identity: X=%.6f want U-I=%.6f (diff %.2e)", got, want, math.Abs(got-want))
	}
}
