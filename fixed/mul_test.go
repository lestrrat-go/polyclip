package fixed

import (
	"math"
	"testing"
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
		if got.Hi != c.hi || got.Lo != c.lo {
			t.Errorf("MulI64(%d, %d) = {Hi:%d Lo:%d} want {Hi:%d Lo:%d}",
				c.a, c.b, got.Hi, got.Lo, c.hi, c.lo)
		}
	}
}

func TestMulI64Large(t *testing.T) {
	// 2^60 * 2^60 = 2^120. In I128 that's Hi = 2^56, Lo = 0.
	a := int64(1) << 60
	got := MulI64(a, a)
	if got.Hi != (int64(1)<<56) || got.Lo != 0 {
		t.Errorf("MulI64(2^60, 2^60) = {Hi:%d Lo:%d} want {Hi:%d Lo:0}",
			got.Hi, got.Lo, int64(1)<<56)
	}
	// And negative variant: (-2^60) * 2^60 = -2^120
	got = MulI64(-a, a)
	if got.Sign() != -1 {
		t.Errorf("MulI64(-2^60, 2^60).Sign() = %d want -1", got.Sign())
	}
}

func TestMulI64MinInt(t *testing.T) {
	// math.MinInt64 * 1 should equal math.MinInt64 (Hi=-1, Lo=2^63).
	got := MulI64(math.MinInt64, 1)
	if got.Hi != -1 || got.Lo != 1<<63 {
		t.Errorf("MulI64(MinInt64, 1) = {Hi:%d Lo:%d}", got.Hi, got.Lo)
	}
	if got.Sign() != -1 {
		t.Errorf("MulI64(MinInt64, 1).Sign() = %d want -1", got.Sign())
	}
}

func TestI128SubAdd(t *testing.T) {
	// (a*b) - (a*b) == 0
	a, b := int64(123456789), int64(987654321)
	z := MulI64(a, b).Sub(MulI64(a, b))
	if !z.IsZero() {
		t.Errorf("self-subtract not zero: %+v", z)
	}
	// AddSubInverse: x.Add(y).Sub(y) == x
	x := MulI64(5, 7)
	y := MulI64(11, 13)
	got := x.Add(y).Sub(y)
	if got != x {
		t.Errorf("Add+Sub inverse: got %+v want %+v", got, x)
	}
	// Cross-zero subtraction.
	one := MulI64(1, 1)
	negOne := MulI64(1, 1).Sub(MulI64(2, 1)) // 1 - 2 = -1
	if negOne.Sign() != -1 {
		t.Errorf("1-2 sign: %d", negOne.Sign())
	}
	if negOne.Add(one).Sign() != 0 {
		t.Errorf("-1+1 should be zero, got %+v", negOne.Add(one))
	}
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
		if got := Orient2D(c.p, c.q, c.r); got != c.want {
			t.Errorf("%s: Orient2D(%v,%v,%v) = %d want %d", c.name, c.p, c.q, c.r, got, c.want)
		}
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
	if got := Orient2D(p, q, rUp); got != +1 {
		t.Errorf("rUp (off by +1y at max coord) got %d want +1", got)
	}
	if got := Orient2D(p, q, rDown); got != -1 {
		t.Errorf("rDown got %d want -1", got)
	}
	if got := Orient2D(p, q, rOnLine); got != 0 {
		t.Errorf("rOnLine got %d want 0", got)
	}
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
				if got := Orient2D(p, q, r); got != want {
					t.Errorf("Orient2D(%v,%v,%v)=%d, float says %d", p, q, r, got, want)
				}
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
