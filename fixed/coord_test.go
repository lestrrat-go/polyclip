package fixed

import (
	"math"
	"testing"
)

func TestScaleFromBBoxRange(t *testing.T) {
	// Standard slicer-style bbox: 200 mm across, centred at (0,0).
	s := ScaleFromBBox(-100, -100, 100, 100)
	if s.OffsetX != 0 || s.OffsetY != 0 {
		t.Fatalf("Offset: %+v want (0,0)", s)
	}
	// Snap a corner and verify it's within the target magnitude.
	corner := s.Snap(100, 100)
	if abs64(int64(corner.X)) > MaxCoordMagnitude {
		t.Fatalf("|corner.X|=%d exceeds MaxCoordMagnitude=%d", corner.X, MaxCoordMagnitude)
	}
	if abs64(int64(corner.Y)) > MaxCoordMagnitude {
		t.Fatalf("|corner.Y|=%d exceeds MaxCoordMagnitude=%d", corner.Y, MaxCoordMagnitude)
	}
	// Factor must be a power of two for exactness on coarse grids.
	if math.Trunc(math.Log2(s.Factor)) != math.Log2(s.Factor) {
		t.Errorf("Factor=%v is not a power of two", s.Factor)
	}
}

func TestScaleRoundTrip(t *testing.T) {
	s := ScaleFromBBox(-50, -50, 50, 50)
	for _, p := range []struct{ x, y float64 }{
		{0, 0},
		{25.5, -12.25},
		{-49.999, 49.999},
		{50, 50},
	} {
		fx := s.Snap(p.x, p.y)
		gx, gy := s.Unsnap(fx)
		// The round-trip error is bounded by 1 unit at the engine grid
		// resolution, i.e. 1/Factor in user units.
		tol := 1.0 / s.Factor
		if math.Abs(gx-p.x) > tol || math.Abs(gy-p.y) > tol {
			t.Errorf("round-trip (%v,%v): got (%v,%v) tol=%v", p.x, p.y, gx, gy, tol)
		}
	}
}

func TestScaleOffsetForOffsetBBox(t *testing.T) {
	// A bbox far from origin should still be centred on its midpoint so
	// the integer coordinates remain bounded.
	s := ScaleFromBBox(1000, 2000, 1010, 2010)
	if s.OffsetX != 1005 || s.OffsetY != 2005 {
		t.Fatalf("Offset: %+v want (1005,2005)", s)
	}
	p := s.Snap(1005, 2005)
	if p.X != 0 || p.Y != 0 {
		t.Errorf("Snap of midpoint: %+v want (0,0)", p)
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
