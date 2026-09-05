package geom

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPointVectorOps(t *testing.T) {
	p := Point{X: 3, Y: 4}
	q := Point{X: 1, Y: 2}

	require.Equal(t, Point{X: 4, Y: 6}, p.Add(q))
	require.Equal(t, Point{X: 2, Y: 2}, p.Sub(q))
	require.Equal(t, Point{X: 6, Y: 8}, p.Scale(2))
	require.Equal(t, Point{X: -3, Y: -4}, p.Neg())
	require.Equal(t, Point{}, p.Scale(0))

	// Add and Sub undo each other.
	require.Equal(t, p, p.Add(q).Sub(q))
}

func TestPointVectorOpsDropZ(t *testing.T) {
	// Z is auxiliary data attached to a location, not a coordinate the
	// arithmetic means, so a result never claims one. Sub and Neg already did
	// this; Add and Scale match them.
	p := Point{X: 1, Y: 1, Z: 9}
	q := Point{X: 1, Y: 1, Z: 7}

	require.Zero(t, p.Add(q).Z)
	require.Zero(t, p.Sub(q).Z)
	require.Zero(t, p.Scale(2).Z)
	require.Zero(t, p.Neg().Z)
}

func TestPointDist(t *testing.T) {
	require.Equal(t, 5.0, Point{X: 0, Y: 0}.Dist(Point{X: 3, Y: 4}))
	require.Equal(t, 0.0, Point{X: 2, Y: 2}.Dist(Point{X: 2, Y: 2}))

	// Dist2 is the square of it, for the callers that only compare.
	p, q := Point{X: 1, Y: 2}, Point{X: 4, Y: 6}
	require.InDelta(t, p.Dist2(q), p.Dist(q)*p.Dist(q), 1e-12)

	// Hypot holds where the squares would overflow.
	huge := Point{X: 3e200, Y: 4e200}
	require.InEpsilon(t, 5e200, Point{}.Dist(huge), 1e-12)
	require.True(t, math.IsInf(huge.Dist2(Point{}), 1), "Dist2 overflows where Dist does not")
}

func TestPointNormalize(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   Point
		want Point
	}{
		{name: "along x", in: Point{X: 5}, want: Point{X: 1}},
		{name: "along negative y", in: Point{Y: -2}, want: Point{Y: -1}},
		{name: "3-4-5", in: Point{X: 3, Y: 4}, want: Point{X: 0.6, Y: 0.8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.in.Normalize()
			require.True(t, ok)
			require.InDelta(t, tc.want.X, got.X, 1e-15)
			require.InDelta(t, tc.want.Y, got.Y, 1e-15)
			require.InDelta(t, 1.0, got.Len(), 1e-15)
		})
	}
}

func TestPointNormalizeHasNoDirection(t *testing.T) {
	// Each of these would hand back a NaN or an infinity wearing the shape of a
	// unit vector, so each reports false instead.
	for _, tc := range []struct {
		name string
		in   Point
	}{
		{name: "zero vector", in: Point{}},
		{name: "infinite", in: Point{X: math.Inf(1), Y: 0}},
		{name: "both infinite", in: Point{X: math.Inf(1), Y: math.Inf(-1)}},
		{name: "NaN", in: Point{X: math.NaN(), Y: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.in.Normalize()
			require.False(t, ok)
			require.Equal(t, Point{}, got)
		})
	}
}

func TestPointNormalizeAtTheExtremes(t *testing.T) {
	// A vector whose squared length overflows still has a direction, which is
	// why Len goes through Hypot.
	got, ok := Point{X: 3e300, Y: 4e300}.Normalize()
	require.True(t, ok)
	require.InDelta(t, 0.6, got.X, 1e-12)
	require.InDelta(t, 0.8, got.Y, 1e-12)

	// And so does a subnormal one.
	got, ok = Point{X: 5e-320, Y: 0}.Normalize()
	require.True(t, ok)
	require.InDelta(t, 1.0, got.X, 1e-9)
}

func TestPointEqual(t *testing.T) {
	p := Point{X: 1, Y: 1}

	require.True(t, p.Equal(p, 0))
	require.True(t, p.Equal(Point{X: 1.0001, Y: 1}, 1e-3))
	require.False(t, p.Equal(Point{X: 1.0001, Y: 1}, 1e-6))

	// Z is not part of the location the comparison judges.
	require.True(t, p.Equal(Point{X: 1, Y: 1, Z: 42}, 0))

	// Symmetric, and a tolerance that is not a real bound admits nothing.
	q := Point{X: 2, Y: 2}
	require.Equal(t, p.Equal(q, 2), q.Equal(p, 2))
	require.False(t, p.Equal(q, -1))
	require.False(t, p.Equal(q, math.NaN()))
	require.False(t, p.Equal(p, math.NaN()))

	// A coordinate that is not finite is not a location.
	inf := Point{X: math.Inf(1), Y: 0}
	require.False(t, inf.Equal(inf, math.Inf(1)))
	require.False(t, inf.Equal(p, math.Inf(1)))
}

func TestPolygonPerimeter(t *testing.T) {
	square := Polygon{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}
	// The closing edge counts: four sides of 2, not three.
	require.InDelta(t, 8.0, square.Perimeter(), 1e-12)

	// Winding does not change a length, where it does change a signed area.
	reversed := Polygon{{X: 0, Y: 2}, {X: 2, Y: 2}, {X: 2, Y: 0}, {X: 0, Y: 0}}
	require.InDelta(t, square.Perimeter(), reversed.Perimeter(), 1e-12)
	require.NotEqual(t, square.SignedArea(), reversed.SignedArea())

	tri := Polygon{{X: 0, Y: 0}, {X: 3, Y: 0}, {X: 0, Y: 4}}
	require.InDelta(t, 12.0, tri.Perimeter(), 1e-12)
}

func TestPolygonPerimeterDegenerate(t *testing.T) {
	require.Equal(t, 0.0, Polygon(nil).Perimeter())
	require.Equal(t, 0.0, Polygon{}.Perimeter())
	require.Equal(t, 0.0, Polygon{{X: 1, Y: 1}}.Perimeter())

	// Two points are a degenerate ring, and its perimeter is the segment out
	// and back.
	require.InDelta(t, 4.0, Polygon{{X: 0, Y: 0}, {X: 2, Y: 0}}.Perimeter(), 1e-12)
}

func TestPolylineLength(t *testing.T) {
	// An open path has no closing edge, which is the whole difference from
	// Polygon.Perimeter over the same points.
	pts := []Point{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}
	require.InDelta(t, 6.0, Polyline(pts).Length(), 1e-12)
	require.InDelta(t, 8.0, Polygon(pts).Perimeter(), 1e-12)

	require.Equal(t, 0.0, Polyline(nil).Length())
	require.Equal(t, 0.0, Polyline{{X: 1, Y: 1}}.Length())
	require.InDelta(t, 5.0, Polyline{{X: 0, Y: 0}, {X: 3, Y: 4}}.Length(), 1e-12)
}
