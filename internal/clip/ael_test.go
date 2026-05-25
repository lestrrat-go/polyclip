package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
)

func ae(x int64, seg *Segment) *ActiveEdge {
	return &ActiveEdge{Seg: seg, CurrX: fixed.Coord(x)}
}

func TestAELInsertOrder(t *testing.T) {
	a := NewAEL()
	s1, s2, s3 := seg(0, 0, 0, 10), seg(5, 0, 5, 10), seg(10, 0, 10, 10)

	a.Insert(ae(10, &s3))
	a.Insert(ae(0, &s1))
	a.Insert(ae(5, &s2))

	require.Equal(t, 3, a.Len(), "Len: %d want 3", a.Len())
	for i, want := range []fixed.Coord{0, 5, 10} {
		got := a.At(i).CurrX
		require.Equal(t, want, got, "[%d].CurrX = %d want %d", i, got, want)
	}
}

func TestAELRemove(t *testing.T) {
	a := NewAEL()
	s1, s2, s3 := seg(0, 0, 0, 10), seg(5, 0, 5, 10), seg(10, 0, 10, 10)
	e1, e2, e3 := ae(0, &s1), ae(5, &s2), ae(10, &s3)
	a.Insert(e1)
	a.Insert(e2)
	a.Insert(e3)

	a.Remove(e2)
	require.True(t, a.Len() == 2 && a.At(0).CurrX == 0 && a.At(1).CurrX == 10, "post-remove: %d edges, [0]=%d [1]=%d", a.Len(), a.At(0).CurrX, a.At(1).CurrX)

	// Remove non-present is a no-op.
	a.Remove(e2)
	require.Equal(t, 2, a.Len(), "removing absent edge changed Len: %d", a.Len())
}

func TestAELSwapAt(t *testing.T) {
	a := NewAEL()
	s1, s2 := seg(0, 0, 0, 10), seg(5, 0, 5, 10)
	e1, e2 := ae(0, &s1), ae(5, &s2)
	a.Insert(e1)
	a.Insert(e2)
	a.SwapAt(0)
	require.True(t, a.At(0) == e2 && a.At(1) == e1, "after SwapAt: [0]=%p [1]=%p want %p %p", a.At(0), a.At(1), e2, e1)
}

func TestAELNeighbors(t *testing.T) {
	a := NewAEL()
	s1, s2, s3 := seg(0, 0, 0, 10), seg(5, 0, 5, 10), seg(10, 0, 10, 10)
	e1, e2, e3 := ae(0, &s1), ae(5, &s2), ae(10, &s3)
	a.Insert(e1)
	a.Insert(e2)
	a.Insert(e3)

	require.Nil(t, a.LeftOf(0), "LeftOf(0): %v want nil", a.LeftOf(0))
	require.Nil(t, a.RightOf(2), "RightOf(2): %v want nil", a.RightOf(2))
	require.Same(t, e1, a.LeftOf(1), "LeftOf(1): %p want %p", a.LeftOf(1), e1)
	require.Same(t, e3, a.RightOf(1), "RightOf(1): %p want %p", a.RightOf(1), e3)
}

func TestAELTieBreakBySlope(t *testing.T) {
	// Two edges with the same CurrX at the current scanline (both pass
	// through the same point). The flatter edge should sort left of the
	// steeper edge.
	a := NewAEL()
	// Both edges go through (5, 0). Edge "shallow" continues to (15, 10),
	// "steep" continues to (6, 10). Shallow slope = 1.0, steep slope = 0.1.
	shallow := seg(5, 0, 15, 10) // dx/dy = 1.0
	steep := seg(5, 0, 6, 10)    // dx/dy = 0.1

	eShallow := ae(5, &shallow)
	eSteep := ae(5, &steep)

	a.Insert(eShallow)
	a.Insert(eSteep)
	// Steep slope (0.1) < shallow slope (1.0), so steep comes first.
	require.Same(t, eSteep, a.At(0), "tie-break order wrong: At(0)=%p want %p (steep)", a.At(0), eSteep)
}

func TestAELUpdateForScanline(t *testing.T) {
	a := NewAEL()
	// Diagonal segment from (0,0) to (10,10).
	s := seg(0, 0, 10, 10)
	e := ae(0, &s)
	a.Insert(e)
	a.UpdateForScanline(5)
	require.Equal(t, fixed.Coord(5), e.CurrX, "CurrX after scanline=5: %d want 5", e.CurrX)
	a.UpdateForScanline(8)
	require.Equal(t, fixed.Coord(8), e.CurrX, "CurrX after scanline=8: %d want 8", e.CurrX)
}

func TestXAtYHorizontal(t *testing.T) {
	s := seg(2, 5, 8, 5)
	got := XAtY(&s, 5)
	require.Equal(t, fixed.Coord(2), got, "horizontal XAtY: %d want 2", got)
}

func TestXAtYDiagonal(t *testing.T) {
	s := seg(0, 0, 10, 20)
	for _, c := range []struct {
		y, want int64
	}{
		{0, 0},
		{10, 5},
		{20, 10},
	} {
		got := XAtY(&s, fixed.Coord(c.y))
		require.Equal(t, c.want, int64(got), "XAtY(y=%d) = %d want %d", c.y, got, c.want)
	}
}

func TestCmpXAtY(t *testing.T) {
	// a: (0,0)->(10,10), b: (10,0)->(0,10). They cross at (5,5).
	a := seg(0, 0, 10, 10)
	b := seg(10, 0, 0, 10)
	require.True(t, cmpXAtY(&a, &b, 2) < 0, "at y=2 a should be left of b, got %d", cmpXAtY(&a, &b, 2))
	require.Equal(t, 0, cmpXAtY(&a, &b, 5), "at y=5 (crossing) a and b should be equal, got %d", cmpXAtY(&a, &b, 5))
	require.True(t, cmpXAtY(&a, &b, 8) > 0, "at y=8 a should be right of b, got %d", cmpXAtY(&a, &b, 8))
}
