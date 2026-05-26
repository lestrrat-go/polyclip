package geom

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuilderSingleRegion(t *testing.T) {
	m, err := New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		Build()
	require.NoError(t, err)
	require.Len(t, m, 1)
	require.Empty(t, m[0].Holes)
	require.True(t, m[0].Outer.IsCCW(), "outer should be normalized CCW")
	require.Equal(t, 100.0, m.Area())
}

func TestBuilderNormalizesWinding(t *testing.T) {
	// Outer given CW, hole given CCW; Build must flip both to canonical.
	hole := Polygon{{X: 3, Y: 3}, {X: 6, Y: 3}, {X: 6, Y: 6}, {X: 3, Y: 6}} // CCW
	m := New().
		Point(0, 0).Point(0, 10).Point(10, 10).Point(10, 0). // CW outer
		Hole(hole).
		MustBuild()
	require.Len(t, m, 1)
	require.True(t, m[0].Outer.IsCCW(), "outer normalized to CCW")
	require.Len(t, m[0].Holes, 1)
	require.False(t, m[0].Holes[0].IsCCW(), "hole normalized to CW")
	require.Equal(t, 100.0-9.0, m.Area())
}

func TestBuilderNextPiece(t *testing.T) {
	m := New().
		Point(0, 0).Point(2, 0).Point(2, 2).Point(0, 2).
		NextPiece().
		Point(10, 10).Point(14, 10).Point(14, 14).Point(10, 14).
		Hole(Polygon{{X: 11, Y: 11}, {X: 13, Y: 11}, {X: 13, Y: 13}}).
		MustBuild()
	require.Len(t, m, 2)
	require.Empty(t, m[0].Holes)
	require.Len(t, m[1].Holes, 1)
}

func TestBuilderNextPieceNoOpWhenEmpty(t *testing.T) {
	// Leading and repeated NextPiece on an empty current piece do nothing.
	m := New().NextPiece().NextPiece().
		Point(0, 0).Point(2, 0).Point(2, 2).
		MustBuild()
	require.Len(t, m, 1, "leading NextPiece calls must not create empty pieces")

	empty, err := New().NextPiece().Build()
	require.NoError(t, err)
	require.Empty(t, empty)
}

func TestBuilderTrailingNextPieceDropped(t *testing.T) {
	m := New().
		Point(0, 0).Point(2, 0).Point(2, 2).
		NextPiece(). // trailing: creates an empty piece that Build drops
		MustBuild()
	require.Len(t, m, 1)
}

func TestBuilderNextPieceDynamicLoop(t *testing.T) {
	// NextPiece's no-op-when-empty lets the loop call it every iteration with
	// no first-iteration guard.
	pieces := [][]Point{
		{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}},
		{{X: 10, Y: 10}, {X: 12, Y: 10}, {X: 12, Y: 12}},
		{{X: 20, Y: 20}, {X: 22, Y: 20}, {X: 22, Y: 22}},
	}
	b := New()
	for _, pc := range pieces {
		b.NextPiece()
		for _, p := range pc {
			b.Point(p.X, p.Y)
		}
	}
	m := b.MustBuild()
	require.Len(t, m, 3)
}

func TestBuilderDegenerateRing(t *testing.T) {
	_, err := New().Point(0, 0).Point(1, 1).Build()
	require.Error(t, err, "two-point outer ring should fail")

	_, err = New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		Hole(Polygon{{X: 1, Y: 1}, {X: 2, Y: 2}}). // 2-point hole
		Build()
	require.Error(t, err, "two-point hole should fail")
}

func TestBuilderHoleWithoutOuter(t *testing.T) {
	_, err := New().Hole(Polygon{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 2, Y: 2}}).Build()
	require.ErrorContains(t, err, "holes but no outer ring")
}

func TestBuilderMustBuildPanics(t *testing.T) {
	require.Panics(t, func() {
		New().Point(0, 0).Point(1, 1).MustBuild()
	})
}

func TestBuilderHoleDynamicSpread(t *testing.T) {
	// The motivating case: a runtime-sized []Polygon spreads straight in.
	holes := []Polygon{
		{{X: 1, Y: 1}, {X: 2, Y: 1}, {X: 2, Y: 2}},
		{{X: 5, Y: 5}, {X: 6, Y: 5}, {X: 6, Y: 6}},
	}
	m := New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		Hole(holes...).
		MustBuild()
	require.Len(t, m[0].Holes, 2)
}

func TestBuilderHoleCopiesRing(t *testing.T) {
	ring := Polygon{{X: 3, Y: 3}, {X: 6, Y: 3}, {X: 5, Y: 6}}
	main := New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).Hole(ring)
	ring[0] = Point{X: 99, Y: 99} // mutate caller slice after attaching
	m := main.MustBuild()
	require.NotEqual(t, 99.0, m[0].Holes[0][0].X, "Hole must copy the ring, not alias it")
}

func TestBuilderPolygon(t *testing.T) {
	// Fluently build a ring, hand it to Hole as a Polygon.
	hole := New().Point(3, 3).Point(6, 3).Point(5, 6).MustPolygon()
	require.Equal(t, Polygon{{X: 3, Y: 3}, {X: 6, Y: 3}, {X: 5, Y: 6}}, hole)
	m := New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		Hole(hole).
		MustBuild()
	require.Len(t, m[0].Holes, 1)
}

func TestBuilderPolygonErrors(t *testing.T) {
	_, err := New().Point(0, 0).Point(1, 0).Build() // not relevant; just exercising
	require.Error(t, err)

	_, err = New().Point(0, 0).Point(1, 0).Polygon() // < 3 points
	require.Error(t, err, "ring with two points should fail")

	_, err = New().Point(0, 0).Point(1, 0).Point(1, 1).
		NextPiece().Point(5, 5).Point(6, 5).Point(6, 6).
		Polygon() // multiple pieces
	require.Error(t, err, "multi-piece builder is not a single ring")

	_, err = New().Point(0, 0).Point(2, 0).Point(2, 2).
		Hole(Polygon{{X: 0.5, Y: 0.5}, {X: 1, Y: 0.5}, {X: 1, Y: 1}}).
		Polygon() // has a hole
	require.Error(t, err, "ring with a hole is not a plain Polygon")
}

func TestBuilderBuildIdempotent(t *testing.T) {
	b := New().Point(0, 0).Point(0, 10).Point(10, 10).Point(10, 0) // CW
	m1, err := b.Build()
	require.NoError(t, err)
	m2, err := b.Build()
	require.NoError(t, err)
	require.Equal(t, m1, m2, "repeated Build should be stable")
	require.True(t, m2[0].Outer.IsCCW())
}

func TestBuilderEmpty(t *testing.T) {
	m, err := New().Build()
	require.NoError(t, err)
	require.Empty(t, m)
}
