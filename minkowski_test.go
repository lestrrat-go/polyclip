package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// square2 is a 2x2 axis-aligned pattern anchored at the origin corner.
var square2 = geom.Polygon{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}

// TestMinkowskiSumOpenSegment sweeps the pattern along a single horizontal
// open segment: the 2x2 square translated from (0,0) to (10,0) fills the
// rectangle [0,12]x[0,2].
func TestMinkowskiSumOpenSegment(t *testing.T) {
	got, err := MinkowskiSum(square2, []geom.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, false)
	require.NoError(t, err)
	require.Len(t, got, 1, "got %d pieces, want 1", len(got))
	require.InDelta(t, 24, got.Area(), 1e-9, "area = %v, want 24", got.Area())
	b := bboxOf([]geom.Polygon{got[0].Outer})
	require.True(t, b.Min == (geom.Point{X: 0, Y: 0}) && b.Max == (geom.Point{X: 12, Y: 2}), "bbox = %+v, want [0,0]-[12,2]", b)
}

// TestMinkowskiDiffOpenSegment reflects the pattern through the origin, so the
// same segment sweep fills [-2,10]x[-2,0] (area 24).
func TestMinkowskiDiffOpenSegment(t *testing.T) {
	got, err := MinkowskiDiff(square2, []geom.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, false)
	require.NoError(t, err)
	require.InDelta(t, 24, got.Area(), 1e-9, "area = %v, want 24", got.Area())
	b := bboxOf([]geom.Polygon{got[0].Outer})
	require.True(t, b.Min == (geom.Point{X: -2, Y: -2}) && b.Max == (geom.Point{X: 10, Y: 0}), "bbox = %+v, want [-2,-2]-[10,0]", b)
}

// TestMinkowskiSumClosedSquareFrame sweeps the pattern around a closed 10x10
// square boundary. The boundary band is one ExPolygon with a single hole: the
// outer extent is [0,12]x[0,12] (area 144) and the uncovered interior is the
// [2,10]x[2,10] hole (area 64), leaving a frame of area 80.
func TestMinkowskiSumClosedSquareFrame(t *testing.T) {
	path := []geom.Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	got, err := MinkowskiSum(square2, path, true)
	require.NoError(t, err)
	require.Len(t, got, 1, "got %d pieces, want 1", len(got))
	require.Len(t, got[0].Holes, 1, "got %d holes, want 1", len(got[0].Holes))
	require.InDelta(t, 80, got.Area(), 1e-9, "area = %v, want 80", got.Area())
	require.InDelta(t, 144, got[0].Outer.Area(), 1e-9, "outer area = %v, want 144", got[0].Outer.Area())
	require.InDelta(t, 64, got[0].Holes[0].Area(), 1e-9, "hole area = %v, want 64", got[0].Holes[0].Area())
}

// TestMinkowskiEmptyInputs returns an empty result for an empty pattern or path.
func TestMinkowskiEmptyInputs(t *testing.T) {
	seg := []geom.Point{{X: 0, Y: 0}, {X: 1, Y: 0}}
	got, err := MinkowskiSum(geom.Polygon{}, seg, false)
	require.True(t, err == nil && len(got) == 0, "empty pattern: got %v, %v; want empty, nil", got, err)
	got, err = MinkowskiSum(square2, nil, false)
	require.True(t, err == nil && len(got) == 0, "empty path: got %v, %v; want empty, nil", got, err)
}
