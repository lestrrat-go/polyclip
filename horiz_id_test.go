package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// TestHorizIdentityRepro is the regression for the §7.6 axis-aligned Intersect
// spurious-lobe bug. A and B share the collinear boundary segment (1,1)-(2,1);
// the true intersection is the unit square [1,2]x[0,1] (area 1). The sweep used
// to emit a second, spurious triangle lobe (2,1)-(3,3)-(2,3) lying inside B's
// upper-right region but OUTSIDE A, so Intersect returned area 2 and the U/D/X
// algebraic identities (computed off that wrong I) broke. The figure-8 formed
// because at the shared edge — A's outer local maximum — B's hot bound was
// dragged up out of A instead of the ring closing. Fixed by closing the cross-
// source ring at a coincident horizontal apex when the other source does not
// fill above it (clip/sweep.go closeBound self-closure, DESIGN.md §7.6).
func TestHorizIdentityRepro(t *testing.T) {
	a := geom.MultiPolygon{geom.ExPolygon{Outer: geom.Polygon{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 1}, {X: 1, Y: 1}, {X: 0, Y: 1},
	}}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.Polygon{
		{X: 1, Y: -1}, {X: 3, Y: -1}, {X: 3, Y: 3}, {X: 2, Y: 3}, {X: 2, Y: 1}, {X: 1, Y: 1},
	}}}
	u, _ := Union(a, b)
	i, _ := Intersect(a, b)
	d, _ := Difference(a, b)
	x, _ := Xor(a, b)
	aA, bA := a.Area(), b.Area()
	uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
	t.Logf("A=%v B=%v U=%v I=%v D=%v X=%v", aA, bA, uA, iA, dA, xA)
	// The intersection is the unit square [1,2]x[0,1]; the spurious triangle
	// lobe (which made I=2) must be gone.
	require.InDelta(t, 1, iA, 1e-6, "intersect area: got %v want 1 (spurious lobe?)", iA)
	require.InDelta(t, aA+bA-iA, uA, 1e-6, "U identity: U=%v want %v", uA, aA+bA-iA)
	require.InDelta(t, aA-iA, dA, 1e-6, "D identity: D=%v want %v", dA, aA-iA)
	require.InDelta(t, uA-iA, xA, 1e-6, "X identity: X=%v want %v", xA, uA-iA)
}
