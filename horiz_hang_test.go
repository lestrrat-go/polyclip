package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// TestHorizJoinHangRepro is the minimal repro for the processHorzJoins
// infinite loop found by the §7.5 reachability harness: Difference of two
// axis-aligned skyline polygons spins forever in the horizontal-join merge.
func TestHorizJoinHangRepro(t *testing.T) {
	a := geom.MultiPolygon{geom.ExPolygon{Outer: geom.Polygon{
		{X: 0, Y: 0}, {X: 7, Y: 0}, {X: 7, Y: 6}, {X: 6, Y: 6}, {X: 5, Y: 6},
		{X: 5, Y: 2}, {X: 4, Y: 2}, {X: 3, Y: 2}, {X: 3, Y: 4}, {X: 2, Y: 4},
		{X: 2, Y: 6}, {X: 1, Y: 6}, {X: 1, Y: 3}, {X: 0, Y: 3},
	}}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.Polygon{
		{X: 1, Y: 1}, {X: 4, Y: 1}, {X: 4, Y: 2}, {X: 3, Y: 2},
		{X: 3, Y: 4}, {X: 2, Y: 4}, {X: 1, Y: 4},
	}}}
	require.Empty(t, a.Validate(), "A invalid: %v", a.Validate())
	require.Empty(t, b.Validate(), "B invalid: %v", b.Validate())
	got, err := Difference(a, b)
	require.NoError(t, err)
	t.Logf("Difference area=%v result=%v", got.Area(), got)
}
