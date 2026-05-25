package polyclip

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOffsetEmpty(t *testing.T) {
	_, err := Offset(MultiPolygon{}, 5, OffsetOptions{})
	require.Equal(t, ErrOffsetEmpty, err, "Offset(empty) err = %v, want ErrOffsetEmpty", err)
}

func TestOffsetZero(t *testing.T) {
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, 0, OffsetOptions{})
	require.NoError(t, err)
	require.InDelta(t, in.Area(), got.Area(), 0.01, "Offset(_, 0) changed area: %v vs %v", got.Area(), in.Area())
}

func TestOffsetSquareOutwardMiter(t *testing.T) {
	// 10x10 square offset outward by 2 with miter joins gives a 14x14
	// square (area 196).
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, 2, OffsetOptions{Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 piece, got %d: %+v", len(got), got)
	wantArea := 14.0 * 14.0
	require.InDelta(t, wantArea, got.Area(), 0.5, "Offset(square, 2, miter) area %v want %v", got.Area(), wantArea)
}

func TestOffsetSquareOutwardRound(t *testing.T) {
	// 10x10 square offset outward by 2 with round joins: the four
	// corners become quarter-circles. Area = 14*14 - 4*4 + π*4 = 196 - 16
	// + 12.566 = 192.566.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, 2, OffsetOptions{Join: JoinRound, ArcTol: 0.05})
	require.NoError(t, err)
	wantArea := 14.0*14.0 - 4.0*4.0 + math.Pi*4.0
	// Round join uses chord approximation — allow looser tolerance.
	require.InDelta(t, wantArea, got.Area(), 2, "Offset(square, 2, round) area %v want %v", got.Area(), wantArea)
}

func TestOffsetSquareOutwardSquare(t *testing.T) {
	// 10x10 square offset outward by 2 with square joins: the four
	// corners become 2x2 squares (45° chamfers from the offset endpoints).
	// Actually square join produces a square corner (same as miter for
	// axial). Area should equal the miter case: 196.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, 2, OffsetOptions{Join: JoinSquare})
	require.NoError(t, err)
	require.False(t, got.Area() < 14*14*0.95 || got.Area() > 14*14*1.05, "Offset(square, 2, square) area %v want ≈196", got.Area())
}

func TestOffsetSquareOutwardBevel(t *testing.T) {
	// 10x10 square offset outward by 2 with bevel joins: each 90° corner is
	// cut by a straight chord between the two offset endpoints, removing a
	// 2x2 right-triangle (area 2) from each corner of the 14x14 miter square.
	// Area = 196 - 4*2 = 188.
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, 2, OffsetOptions{Join: JoinBevel})
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 piece, got %d: %+v", len(got), got)
	wantArea := 188.0
	require.InDelta(t, wantArea, got.Area(), 0.5, "Offset(square, 2, bevel) area %v want %v", got.Area(), wantArea)
}

func TestOffsetSquareInward(t *testing.T) {
	// 10x10 square offset INWARD by 2: 6x6 square (area 36).
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	got, err := Offset(in, -2, OffsetOptions{Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 piece, got %d", len(got))
	wantArea := 6.0 * 6.0
	require.InDelta(t, wantArea, got.Area(), 0.5, "Offset(square, -2) area %v want %v", got.Area(), wantArea)
}

func TestOffsetSquareCollapses(t *testing.T) {
	// 10x10 square offset inward by 6 — collapses (smallest half-extent
	// is 5, so d=-6 should produce empty).
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}}}
	_, err := Offset(in, -6, OffsetOptions{Join: JoinMiter})
	require.Equal(t, ErrOffsetEmpty, err, "Offset(square, -6) err = %v, want ErrOffsetEmpty", err)
}

func TestOffsetRoundTrip(t *testing.T) {
	// Offset out by d then in by -d should approximately recover the
	// original for a convex polygon. Use a regular hexagon to exercise
	// non-axial edges with round joins.
	in := MultiPolygon{ExPolygon{Outer: regularPolygon(0, 0, 10, 6)}}
	d := 2.0
	out, err := Offset(in, d, OffsetOptions{Join: JoinRound, ArcTol: 0.05})
	require.NoError(t, err)
	back, err := Offset(out, -d, OffsetOptions{Join: JoinRound, ArcTol: 0.05})
	require.NoError(t, err)
	// Allow 5% area tolerance (round-trip incurs tessellation loss).
	rel := math.Abs(back.Area()-in.Area()) / in.Area()
	require.False(t, rel > 0.05, "Round-trip area: got %v, in %v, relative %v", back.Area(), in.Area(), rel)
}

// regularPolygon builds a CCW regular polygon with n sides centred at
// (cx, cy) with circumradius r.
func regularPolygon(cx, cy, r float64, n int) Polygon {
	pts := make(Polygon, n)
	for i := range n {
		ang := 2 * math.Pi * float64(i) / float64(n)
		pts[i] = Point{X: cx + r*math.Cos(ang), Y: cy + r*math.Sin(ang)}
	}
	return pts
}
