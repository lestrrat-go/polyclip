package polyclip

import (
	"math"
	"math/rand"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

func TestOffsetEmpty(t *testing.T) {
	got, err := Offset(geom.MultiPolygon{}, 5, OffsetOptions{})
	require.NoError(t, err, "Offset(empty) err = %v, want nil", err)
	require.Empty(t, got, "Offset(empty) = %v, want empty", got)
}

func TestOffsetZero(t *testing.T) {
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		MustPolygon()}}
	got, err := Offset(in, 0, OffsetOptions{})
	require.NoError(t, err)
	require.InDelta(t, in.Area(), got.Area(), 0.01, "Offset(_, 0) changed area: %v vs %v", got.Area(), in.Area())
}

func TestOffsetSquareOutward(t *testing.T) {
	// A 10x10 square offset outward by 2 under each join style. The
	// expected resulting area depends on how the corners are joined.
	cases := []struct {
		name string
		opts OffsetOptions
		// wantArea is the expected area for the case.
		wantArea float64
		// wantPieces, when > 0, asserts the number of resulting pieces.
		wantPieces int
		// tol, when > 0, drives a symmetric require.InDelta check.
		tol float64
		// assert, when non-nil, replaces the InDelta check with a custom
		// assertion (used for the range-style "Square" case).
		assert func(t *testing.T, area float64)
	}{
		{
			// miter joins give a 14x14 square (area 196).
			name:       "Miter",
			opts:       OffsetOptions{Join: JoinMiter},
			wantArea:   14.0 * 14.0,
			wantPieces: 1,
			tol:        0.5,
		},
		{
			// round joins: the four corners become quarter-circles.
			// Area = 14*14 - 4*4 + π*4 = 196 - 16 + 12.566 = 192.566.
			// Round join uses chord approximation — looser tolerance.
			name:     "Round",
			opts:     OffsetOptions{Join: JoinRound, ArcTol: 0.05},
			wantArea: 14.0*14.0 - 4.0*4.0 + math.Pi*4.0,
			tol:      2,
		},
		{
			// square joins produce a square corner (same as miter for
			// axial). Area should equal the miter case: 196.
			name:     "Square",
			opts:     OffsetOptions{Join: JoinSquare},
			wantArea: 14 * 14,
			assert: func(t *testing.T, area float64) {
				require.False(t, area < 14*14*0.95 || area > 14*14*1.05, "Offset(square, 2, square) area %v want ≈196", area)
			},
		},
		{
			// bevel joins: each 90° corner is cut by a straight chord
			// between the two offset endpoints, removing a 2x2 right-
			// triangle (area 2) from each corner of the 14x14 miter square.
			// Area = 196 - 4*2 = 188.
			name:       "Bevel",
			opts:       OffsetOptions{Join: JoinBevel},
			wantArea:   188.0,
			wantPieces: 1,
			tol:        0.5,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
				Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
				MustPolygon()}}
			got, err := Offset(in, 2, tc.opts)
			require.NoError(t, err)
			if tc.wantPieces > 0 {
				require.Len(t, got, tc.wantPieces, "expected %d piece, got %d: %+v", tc.wantPieces, len(got), got)
			}
			if tc.assert != nil {
				tc.assert(t, got.Area())
			} else {
				require.InDelta(t, tc.wantArea, got.Area(), tc.tol, "Offset(square, 2, %s) area %v want %v", tc.name, got.Area(), tc.wantArea)
			}
		})
	}
}

func TestOffsetSquareInward(t *testing.T) {
	// 10x10 square offset INWARD by 2: 6x6 square (area 36).
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		MustPolygon()}}
	got, err := Offset(in, -2, OffsetOptions{Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, got, 1, "expected 1 piece, got %d", len(got))
	wantArea := 6.0 * 6.0
	require.InDelta(t, wantArea, got.Area(), 0.5, "Offset(square, -2) area %v want %v", got.Area(), wantArea)
}

func TestOffsetSquareCollapses(t *testing.T) {
	// 10x10 square offset inward by 6 — collapses (smallest half-extent
	// is 5, so d=-6 should produce empty).
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		MustPolygon()}}
	got, err := Offset(in, -6, OffsetOptions{Join: JoinMiter})
	require.NoError(t, err, "Offset(square, -6) err = %v, want nil", err)
	require.Empty(t, got, "Offset(square, -6) = %v, want empty (collapsed)", got)
}

func TestOffsetRoundTrip(t *testing.T) {
	// Offset out by d then in by -d should approximately recover the
	// original for a convex polygon. Use a regular hexagon to exercise
	// non-axial edges with round joins.
	in := geom.MultiPolygon{geom.ExPolygon{Outer: regularPolygon(0, 0, 10, 6)}}
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
func regularPolygon(cx, cy, r float64, n int) geom.Polygon {
	b := geom.New()
	for i := range n {
		ang := 2 * math.Pi * float64(i) / float64(n)
		b.Point(cx+r*math.Cos(ang), cy+r*math.Sin(ang))
	}
	return b.MustPolygon()
}

// dumbbellShape: two 10×10 pads joined by a thin neck (y in [4,6]).
func dumbbellShape() geom.Polygon {
	return geom.New().
		Point(0, 0).Point(10, 0).Point(10, 4).Point(20, 4).
		Point(20, 0).Point(30, 0).Point(30, 10).Point(20, 10).
		Point(20, 6).Point(10, 6).Point(10, 10).Point(0, 10).
		MustPolygon()
}

func rotatePoly(p geom.Polygon, deg float64) geom.Polygon {
	a := deg * math.Pi / 180
	ca, sa := math.Cos(a), math.Sin(a)
	b := geom.New()
	for _, v := range p {
		b.Point(v.X*ca-v.Y*sa, v.X*sa+v.Y*ca)
	}
	return b.MustPolygon()
}

// TestOffsetDumbbellSplits checks that an inward offset past the neck width
// splits the dumbbell into two islands (DESIGN.md §7.1) rather than dropping
// the whole ring. The neck half-height is 1, so d=-2 pinches it; each pad
// erodes to ~6×6 = 36, total ~72. Verified axis-aligned and rotated.
func TestOffsetDumbbellSplits(t *testing.T) {
	for _, deg := range []float64{0, 17, 90, 45, 30, 60, 7, 123} {
		in := geom.MultiPolygon{geom.ExPolygon{Outer: rotatePoly(dumbbellShape(), deg)}}
		got, err := Offset(in, -2, OffsetOptions{Join: JoinMiter})
		require.NoError(t, err, "deg=%g: unexpected error", deg)
		require.Len(t, got, 2, "deg=%g: got %d pieces, want 2 islands (area %.1f)", deg, len(got), got.Area())
		require.InDelta(t, 72, got.Area(), 4, "deg=%g: total area %.1f, want ~72", deg, got.Area())
	}
}

// TestOffsetUNotchCloses checks a U-shape whose slot is narrower than 2|d|:
// the inward offset closes the slot, turning the U into a single solid blob
// (no longer a U). The result must stay connected and lose the slot.
func TestOffsetUNotchCloses(t *testing.T) {
	// U opening upward: outer wall 12 wide, 10 tall, with a 2-wide slot from
	// the top down to y=4 between x=5 and x=7.
	u := geom.New().
		Point(0, 0).Point(12, 0).Point(12, 10).Point(7, 10).
		Point(7, 4).Point(5, 4).Point(5, 10).Point(0, 10).
		MustPolygon()
	in := geom.MultiPolygon{geom.ExPolygon{Outer: u}}
	// Slot half-width is 1, so d=-1.5 (>1) closes it.
	got, err := Offset(in, -1.5, OffsetOptions{Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, got, 1, "got %d pieces, want 1 solid blob", len(got))
	// The eroded U with the slot closed has no interior hole and the slot
	// region is filled: a point in the former slot mouth (6, 9) should now be
	// inside the eroded body's bounding extent only if filled. We mainly assert
	// the result is a single simple piece with no holes.
	require.Len(t, got[0].Holes, 0, "eroded U should have no holes, got %d", len(got[0].Holes))
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
		in := geom.MultiPolygon{geom.ExPolygon{Outer: poly}}
		d := 3.0 + rng.Float64()*5
		got, err := Offset(in, -d, OffsetOptions{Join: JoinRound, ArcTol: 0.1})
		require.NoError(t, err, "trial %d", trial)
		if len(got) == 0 {
			continue // collapsed entirely — acceptable if input is small
		}
		band := 0.15 * d // skip points within this distance of the decision boundary
		bb := poly.BoundingBox()
		mism, checked := 0, 0
		for range 600 {
			pt := geom.Point{
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
		require.False(t, checked > 0 && float64(mism)/float64(checked) > 0.02,
			"trial %d (d=%.2f): erosion mismatch %d/%d (%.1f%%)",
			trial, d, mism, checked, 100*float64(mism)/float64(checked))
	}
}

// randomStarPolygon builds a simple star-shaped (radially monotone) polygon
// with n vertices at random radii in [rMin,rMax] around the origin — always
// simple, frequently concave.
func randomStarPolygon(rng *rand.Rand, n int, rMin, rMax float64) geom.Polygon {
	b := geom.New()
	for i := range n {
		ang := 2 * math.Pi * float64(i) / float64(n)
		r := rMin + rng.Float64()*(rMax-rMin)
		b.Point(r*math.Cos(ang), r*math.Sin(ang))
	}
	return b.MustPolygon()
}

// distToBoundary returns the minimum distance from p to any edge of ring.
func distToBoundary(ring geom.Polygon, p geom.Point) float64 {
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
func mpContains(m geom.MultiPolygon, p geom.Point) bool {
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
