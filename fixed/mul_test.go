package fixed

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMulI64Small(t *testing.T) {
	cases := []struct {
		a, b int64
		hi   int64
		lo   uint64
	}{
		{0, 0, 0, 0},
		{1, 1, 0, 1},
		{-1, 1, -1, math.MaxUint64}, // == -1 in 128-bit
		{-1, -1, 0, 1},
		{2, 3, 0, 6},
		{-2, 3, -1, math.MaxUint64 - 5},
		{1 << 32, 1 << 32, 1, 0}, // 2^64
	}
	for _, c := range cases {
		got := MulI64(c.a, c.b)
		require.Equal(t, c.hi, got.Hi, "MulI64(%d, %d) = {Hi:%d Lo:%d} want {Hi:%d Lo:%d}",
			c.a, c.b, got.Hi, got.Lo, c.hi, c.lo)
		require.Equal(t, c.lo, got.Lo, "MulI64(%d, %d) = {Hi:%d Lo:%d} want {Hi:%d Lo:%d}",
			c.a, c.b, got.Hi, got.Lo, c.hi, c.lo)
	}
}

func TestMulI64Large(t *testing.T) {
	// 2^60 * 2^60 = 2^120. In I128 that's Hi = 2^56, Lo = 0.
	a := int64(1) << 60
	got := MulI64(a, a)
	require.Equal(t, int64(1)<<56, got.Hi, "MulI64(2^60, 2^60) = {Hi:%d Lo:%d} want {Hi:%d Lo:0}",
		got.Hi, got.Lo, int64(1)<<56)
	require.Equal(t, uint64(0), got.Lo, "MulI64(2^60, 2^60) = {Hi:%d Lo:%d} want {Hi:%d Lo:0}",
		got.Hi, got.Lo, int64(1)<<56)
	// And negative variant: (-2^60) * 2^60 = -2^120
	got = MulI64(-a, a)
	require.Equal(t, -1, got.Sign(), "MulI64(-2^60, 2^60).Sign() = %d want -1", got.Sign())
}

func TestMulI64MinInt(t *testing.T) {
	// math.MinInt64 * 1 should equal math.MinInt64 (Hi=-1, Lo=2^63).
	got := MulI64(math.MinInt64, 1)
	require.Equal(t, int64(-1), got.Hi, "MulI64(MinInt64, 1) = {Hi:%d Lo:%d}", got.Hi, got.Lo)
	require.Equal(t, uint64(1<<63), got.Lo, "MulI64(MinInt64, 1) = {Hi:%d Lo:%d}", got.Hi, got.Lo)
	require.Equal(t, -1, got.Sign(), "MulI64(MinInt64, 1).Sign() = %d want -1", got.Sign())
}

func TestI128SubAdd(t *testing.T) {
	// (a*b) - (a*b) == 0
	a, b := int64(123456789), int64(987654321)
	z := MulI64(a, b).Sub(MulI64(a, b))
	require.True(t, z.IsZero(), "self-subtract not zero: %+v", z)
	// AddSubInverse: x.Add(y).Sub(y) == x
	x := MulI64(5, 7)
	y := MulI64(11, 13)
	got := x.Add(y).Sub(y)
	require.Equal(t, x, got, "Add+Sub inverse: got %+v want %+v", got, x)
	// Cross-zero subtraction.
	one := MulI64(1, 1)
	negOne := MulI64(1, 1).Sub(MulI64(2, 1)) // 1 - 2 = -1
	require.Equal(t, -1, negOne.Sign(), "1-2 sign: %d", negOne.Sign())
	require.Equal(t, 0, negOne.Add(one).Sign(), "-1+1 should be zero, got %+v", negOne.Add(one))
}

func TestOrient2D(t *testing.T) {
	type case_ struct {
		p, q, r Point
		want    int
		name    string
	}
	cases := []case_{
		{Point{0, 0}, Point{1, 0}, Point{0, 1}, +1, "CCW unit triangle"},
		{Point{0, 0}, Point{0, 1}, Point{1, 0}, -1, "CW unit triangle"},
		{Point{0, 0}, Point{1, 1}, Point{2, 2}, 0, "collinear diagonal"},
		{Point{0, 0}, Point{2, 0}, Point{1, 0}, 0, "collinear on x-axis"},
		{Point{-5, -5}, Point{5, 5}, Point{-5, 5}, +1, "large CCW"},
		{Point{-5, -5}, Point{5, 5}, Point{5, -5}, -1, "large CW"},
	}
	for _, c := range cases {
		got := Orient2D(c.p, c.q, c.r)
		require.Equal(t, c.want, got, "%s: Orient2D(%v,%v,%v) = %d want %d", c.name, c.p, c.q, c.r, got, c.want)
	}
}

func TestOrient2DLargeCoords(t *testing.T) {
	// At the engine grid max, a tiny CCW perturbation must still be
	// detected exactly. This is the case float64 cross products miss.
	m := Coord(MaxCoordMagnitude)
	p := Point{X: -m, Y: -m}
	q := Point{X: m, Y: m}
	rUp := Point{X: -m, Y: -m + 1}   // 1 unit above line q-p extended
	rDown := Point{X: -m, Y: -m - 1} // 1 unit below
	rOnLine := Point{X: m - 1, Y: m - 1}
	require.Equal(t, +1, Orient2D(p, q, rUp), "rUp (off by +1y at max coord) want +1")
	require.Equal(t, -1, Orient2D(p, q, rDown), "rDown want -1")
	require.Equal(t, 0, Orient2D(p, q, rOnLine), "rOnLine want 0")
}

// Make sure Orient2D agrees with the float64 cross product on small inputs
// where the float computation is exact (sanity check that signs aren't flipped).
func TestOrient2DAgreesWithFloat(t *testing.T) {
	pts := []Point{
		{0, 0}, {10, 0}, {0, 10}, {10, 10}, {-5, -5}, {5, -5}, {3, 7},
	}
	for _, p := range pts {
		for _, q := range pts {
			for _, r := range pts {
				if p == q || q == r || p == r {
					continue
				}
				want := signFloat(
					float64(q.X-p.X)*float64(r.Y-p.Y) -
						float64(q.Y-p.Y)*float64(r.X-p.X),
				)
				got := Orient2D(p, q, r)
				require.Equal(t, want, got, "Orient2D(%v,%v,%v)=%d, float says %d", p, q, r, got, want)
			}
		}
	}
}

func signFloat(x float64) int {
	switch {
	case x > 0:
		return +1
	case x < 0:
		return -1
	default:
		return 0
	}
}

func TestCmpRationals(t *testing.T) {
	i := func(v int64) I128 { return MulI64(v, 1) }
	cases := []struct {
		name           string
		na, da, nb, db int64
		want           int
	}{
		{"half vs third", 1, 2, 1, 3, +1},
		{"third vs half", 1, 3, 1, 2, -1},
		{"equal reduced", 1, 2, 2, 4, 0},
		{"neg vs pos", -1, 2, 1, 3, -1},
		{"both neg", -1, 2, -1, 3, -1},
		{"both neg swapped", -1, 3, -1, 2, +1},
		{"zero vs pos", 0, 5, 1, 7, -1},
		{"zero vs neg", 0, 5, -1, 7, +1},
		{"both zero", 0, 3, 0, 9, 0},
		{"equal whole", 5, 1, 5, 1, 0},
	}
	for _, c := range cases {
		got := CmpRationals(i(c.na), c.da, i(c.nb), c.db)
		require.Equal(t, c.want, got, "%s: CmpRationals(%d/%d, %d/%d) = %d want %d", c.name, c.na, c.da, c.nb, c.db, got, c.want)
	}
}

func TestCmpRationalsLargeExact(t *testing.T) {
	// Magnitudes where na·db and nb·da overflow int64 (~2^118 · 2^61 here), so
	// the comparison must use the 192-bit path. This is the case a float
	// intercept comparison gets wrong.
	big := MulI64(int64(1)<<59, int64(1)<<59) // 2^118, fits I128
	bigPlus1 := big.Add(I128{Lo: 1})          // 2^118 + 1
	// big/3 vs big/2: same positive numerator, larger denom is smaller.
	require.Equal(t, -1, CmpRationals(big, 3, big, 2), "big/3 vs big/2 want -1")
	// (2^118+1)/7 vs 2^118/7: numerator larger by 1 -> greater.
	require.Equal(t, +1, CmpRationals(bigPlus1, 7, big, 7), "(big+1)/7 vs big/7 want +1")
	// Cross-denominator near-tie that float64 (52-bit mantissa) cannot resolve:
	// (m·q)/q vs m/1 are exactly equal for m, q near the grid max.
	m := int64(1) << 60
	q := int64(123456789)
	require.Equal(t, 0, CmpRationals(MulI64(m, q), q, MulI64(m, 1), 1), "(m·q)/q vs m/1 want 0")
	require.Equal(t, +1, CmpRationals(MulI64(m, q).Add(I128{Lo: 1}), q, MulI64(m, 1), 1), "(m·q+1)/q vs m/1 want +1")
}
