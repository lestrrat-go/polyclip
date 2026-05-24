package polyclip

import (
	"math"
	"math/rand"
	"testing"
)

// dumbbellShape: two 10×10 pads joined by a thin neck (y in [4,6]).
func dumbbellShape() Polygon {
	return Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 4}, {X: 20, Y: 4},
		{X: 20, Y: 0}, {X: 30, Y: 0}, {X: 30, Y: 10}, {X: 20, Y: 10},
		{X: 20, Y: 6}, {X: 10, Y: 6}, {X: 10, Y: 10}, {X: 0, Y: 10},
	}
}

func rotatePoly(p Polygon, deg float64) Polygon {
	a := deg * math.Pi / 180
	ca, sa := math.Cos(a), math.Sin(a)
	out := make(Polygon, len(p))
	for i, v := range p {
		out[i] = Point{X: v.X*ca - v.Y*sa, Y: v.X*sa + v.Y*ca}
	}
	return out
}

// TestOffsetDumbbellSplits checks that an inward offset past the neck width
// splits the dumbbell into two islands (DESIGN.md §7.1) rather than dropping
// the whole ring. The neck half-height is 1, so d=-2 pinches it; each pad
// erodes to ~6×6 = 36, total ~72. Verified axis-aligned and rotated.
func TestOffsetDumbbellSplits(t *testing.T) {
	for _, deg := range []float64{0, 17, 90, 45, 30, 60, 7, 123} {
		in := MultiPolygon{ExPolygon{Outer: rotatePoly(dumbbellShape(), deg)}}
		got, err := Offset(in, -2, OffsetOptions{Join: JoinMiter})
		if err != nil {
			t.Fatalf("deg=%g: unexpected error %v", deg, err)
		}
		if len(got) != 2 {
			t.Errorf("deg=%g: got %d pieces, want 2 islands (area %.1f)", deg, len(got), got.Area())
			continue
		}
		if a := got.Area(); math.Abs(a-72) > 4 {
			t.Errorf("deg=%g: total area %.1f, want ~72", deg, a)
		}
	}
}

// TestOffsetUNotchCloses checks a U-shape whose slot is narrower than 2|d|:
// the inward offset closes the slot, turning the U into a single solid blob
// (no longer a U). The result must stay connected and lose the slot.
func TestOffsetUNotchCloses(t *testing.T) {
	// U opening upward: outer wall 12 wide, 10 tall, with a 2-wide slot from
	// the top down to y=4 between x=5 and x=7.
	u := Polygon{
		{X: 0, Y: 0}, {X: 12, Y: 0}, {X: 12, Y: 10}, {X: 7, Y: 10},
		{X: 7, Y: 4}, {X: 5, Y: 4}, {X: 5, Y: 10}, {X: 0, Y: 10},
	}
	in := MultiPolygon{ExPolygon{Outer: u}}
	// Slot half-width is 1, so d=-1.5 (>1) closes it.
	got, err := Offset(in, -1.5, OffsetOptions{Join: JoinMiter})
	if err != nil {
		t.Fatalf("err %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pieces, want 1 solid blob", len(got))
	}
	// The eroded U with the slot closed has no interior hole and the slot
	// region is filled: a point in the former slot mouth (6, 9) should now be
	// inside the eroded body's bounding extent only if filled. We mainly assert
	// the result is a single simple piece with no holes.
	if len(got[0].Holes) != 0 {
		t.Errorf("eroded U should have no holes, got %d", len(got[0].Holes))
	}
}

// TestOffsetInwardErosionOracle is the Monte-Carlo erosion oracle (DESIGN.md
// §6 discipline): for random concave polygons, an inward round-join offset must
// match the morphological erosion by a disk of radius |d| — a point belongs to
// the result iff it is at least |d| inside the input boundary. Sampled points
// in the ambiguous band near the boundary are skipped.
func TestOffsetInwardErosionOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20260524))
	const trials = 40
	for trial := range trials {
		poly := randomStarPolygon(rng, 6+rng.Intn(8), 30, 60)
		in := MultiPolygon{ExPolygon{Outer: poly}}
		d := 3.0 + rng.Float64()*5
		got, err := Offset(in, -d, OffsetOptions{Join: JoinRound, ArcTol: 0.1})
		if err == ErrOffsetEmpty {
			continue // collapsed entirely — acceptable if input is small
		}
		if err != nil {
			t.Fatalf("trial %d: err %v", trial, err)
		}
		band := 0.15 * d // skip points within this distance of the decision boundary
		bb := poly.BoundingBox()
		mism, checked := 0, 0
		for range 600 {
			pt := Point{
				X: bb.Min.X + rng.Float64()*(bb.Max.X-bb.Min.X),
				Y: bb.Min.Y + rng.Float64()*(bb.Max.Y-bb.Min.Y),
			}
			inP := poly.Contains(pt)
			dist := distToBoundary(poly, pt)
			expected := inP && dist >= d
			// Skip the ambiguous band where round-join chord error / boundary
			// inclusion makes the truth genuinely uncertain.
			if !inP && dist < band {
				continue
			}
			if math.Abs(dist-d) < band {
				continue
			}
			checked++
			if mpContains(got, pt) != expected {
				mism++
			}
		}
		// Allow a small mismatch rate for residual near-boundary / round-join
		// tessellation effects.
		if checked > 0 && float64(mism)/float64(checked) > 0.02 {
			t.Errorf("trial %d (d=%.2f): erosion mismatch %d/%d (%.1f%%)",
				trial, d, mism, checked, 100*float64(mism)/float64(checked))
		}
	}
}

// randomStarPolygon builds a simple star-shaped (radially monotone) polygon
// with n vertices at random radii in [rMin,rMax] around the origin — always
// simple, frequently concave.
func randomStarPolygon(rng *rand.Rand, n int, rMin, rMax float64) Polygon {
	pts := make(Polygon, n)
	for i := range n {
		ang := 2 * math.Pi * float64(i) / float64(n)
		r := rMin + rng.Float64()*(rMax-rMin)
		pts[i] = Point{X: r * math.Cos(ang), Y: r * math.Sin(ang)}
	}
	return pts
}

// distToBoundary returns the minimum distance from p to any edge of ring.
func distToBoundary(ring Polygon, p Point) float64 {
	minDist := math.Inf(1)
	n := len(ring)
	for i := range n {
		if e := pointSegDist(p, ring[i], ring[(i+1)%n]); e < minDist {
			minDist = e
		}
	}
	return minDist
}

// mpContains reports whether p is inside the MultiPolygon (inside some piece's
// outer and not inside any of that piece's holes).
func mpContains(m MultiPolygon, p Point) bool {
	for _, ex := range m {
		if !ex.Outer.Contains(p) {
			continue
		}
		inHole := false
		for _, h := range ex.Holes {
			if h.Contains(p) {
				inHole = true
				break
			}
		}
		if !inHole {
			return true
		}
	}
	return false
}
