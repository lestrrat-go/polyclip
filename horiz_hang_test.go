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
	a := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(7, 0).Point(7, 6).Point(6, 6).Point(5, 6).
		Point(5, 2).Point(4, 2).Point(3, 2).Point(3, 4).Point(2, 4).
		Point(2, 6).Point(1, 6).Point(1, 3).Point(0, 3).
		MustPolygon()}}
	b := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(1, 1).Point(4, 1).Point(4, 2).Point(3, 2).
		Point(3, 4).Point(2, 4).Point(1, 4).
		MustPolygon()}}
	require.Empty(t, a.Validate(), "A invalid: %v", a.Validate())
	require.Empty(t, b.Validate(), "B invalid: %v", b.Validate())
	got, err := Difference(a, b)
	require.NoError(t, err)
	t.Logf("Difference area=%v result=%v", got.Area(), got)
}
