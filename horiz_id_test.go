package polyclip

import "testing"

// TestHorizIdentityRepro is the minimal repro for the pre-existing
// axis-aligned Union over-count (DESIGN §7.6): on this validated input Union
// reports area 7 where A+B-I is 6, violating the algebraic identity. Both
// polygons share the collinear edge segment (1,1)-(2,1) on the boundary; the
// sweep double-counts the overlap. Skipped until §7.6 is fixed; un-skip then.
func TestHorizIdentityRepro(t *testing.T) {
	t.Skip("known pre-existing §7.6 axis-aligned Union over-count; un-skip when fixed")
	a := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 1}, {X: 1, Y: 1}, {X: 0, Y: 1},
	}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 1, Y: -1}, {X: 3, Y: -1}, {X: 3, Y: 3}, {X: 2, Y: 3}, {X: 2, Y: 1}, {X: 1, Y: 1},
	}}}
	u, _ := Union(a, b)
	i, _ := Intersect(a, b)
	d, _ := Difference(a, b)
	x, _ := Xor(a, b)
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	t.Logf("A=%v B=%v U=%v I=%v D=%v X=%v  A+B-I=%v", aA, bA, uA, iA, dA, xA, aA+bA-iA)
	if abs(uA-(aA+bA-iA)) > 1e-6 {
		t.Errorf("U identity: U=%v want %v", uA, aA+bA-iA)
	}
	if abs(dA-(aA-iA)) > 1e-6 {
		t.Errorf("D identity: D=%v want %v", dA, aA-iA)
	}
	if abs(xA-(uA-iA)) > 1e-6 {
		t.Errorf("X identity: X=%v want %v", xA, uA-iA)
	}
}
