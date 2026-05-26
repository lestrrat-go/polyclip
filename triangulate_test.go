package polyclip

import (
	"math"
	"math/rand"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// triArea returns the absolute area of a triangle.
func triArea(t Triangle) float64 {
	return math.Abs(orient(t[0], t[1], t[2])) / 2
}

// triSumArea returns the total area of a triangle list.
func triSumArea(ts []Triangle) float64 {
	var a float64
	for _, t := range ts {
		a += triArea(t)
	}
	return a
}

// triCentroid returns the centroid of a triangle.
func triCentroid(t Triangle) geom.Point {
	return geom.Point{
		X: (t[0].X + t[1].X + t[2].X) / 3,
		Y: (t[0].Y + t[1].Y + t[2].Y) / 3,
	}
}

func TestTriangulateSquare(t *testing.T) {
	m := geom.MultiPolygon{{Outer: geom.New().Point(0, 0).Point(4, 0).Point(4, 4).Point(0, 4).MustPolygon()}}
	tris := Triangulate(m)
	require.Len(t, tris, 2, "got %d triangles, want 2", len(tris))
	require.InDelta(t, 16.0, triSumArea(tris), 1e-9, "area %v, want %v", triSumArea(tris), 16.0)
	for i, tri := range tris {
		require.Greater(t, orient(tri[0], tri[1], tri[2]), 0.0, "triangle %d not CCW: %v", i, tri)
	}
}

func TestTriangulateTriangle(t *testing.T) {
	m := geom.MultiPolygon{{Outer: geom.New().Point(0, 0).Point(6, 0).Point(0, 6).MustPolygon()}}
	tris := Triangulate(m)
	require.Len(t, tris, 1, "got %d triangles, want 1", len(tris))
	require.InDelta(t, 18.0, triSumArea(tris), 1e-9, "area %v, want 18", triSumArea(tris))
}

func TestTriangulateCWInputNormalized(t *testing.T) {
	// Clockwise outer ring must be normalized to CCW output.
	m := geom.MultiPolygon{{Outer: geom.New().Point(0, 0).Point(0, 4).Point(4, 4).Point(4, 0).MustPolygon()}}
	tris := Triangulate(m)
	require.InDelta(t, 16.0, triSumArea(tris), 1e-9, "area %v, want 16", triSumArea(tris))
	for i, tri := range tris {
		require.Greater(t, orient(tri[0], tri[1], tri[2]), 0.0, "triangle %d not CCW: %v", i, tri)
	}
}

func TestTriangulateConcave(t *testing.T) {
	// An L / arrow shape with a reflex vertex.
	m := geom.MultiPolygon{{Outer: geom.New().
		Point(0, 0).Point(6, 0).Point(6, 2).Point(2, 2).Point(2, 6).Point(0, 6).
		MustPolygon()}}
	tris := Triangulate(m)
	require.InDelta(t, m.Area(), triSumArea(tris), 1e-9, "area %v, want %v", triSumArea(tris), m.Area())
	for i, tri := range tris {
		c := triCentroid(tri)
		require.True(t, m.Contains(c), "triangle %d centroid %v outside region", i, c)
	}
}

func TestTriangulateHolesAndPieces(t *testing.T) {
	cases := []struct {
		name string
		m    geom.MultiPolygon
		// wantArea, when set, overrides the default expected area of m.Area().
		wantArea *float64
		// checkCCW asserts each triangle is wound counter-clockwise.
		checkCCW bool
		// checkCentroid asserts each triangle's centroid lies inside the region.
		checkCentroid bool
	}{
		{
			name: "WithHole",
			m: geom.MultiPolygon{{
				Outer: geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(),
				Holes: []geom.Polygon{geom.New().Point(3, 3).Point(3, 7).Point(7, 7).Point(7, 3).MustPolygon()}, // CW hole
			}},
			wantArea:      func() *float64 { v := 100.0 - 16.0; return &v }(),
			checkCCW:      true,
			checkCentroid: true,
		},
		{
			// A hole pinched against the outer boundary at two shared vertices — a
			// real (non-normalized) boolean-engine output. The robust fallbacks must
			// still cover the region exactly without overlap.
			name: "TouchingHole",
			m: geom.MultiPolygon{{
				Outer: geom.New().Point(11, 8).Point(7, 8).Point(6, 8).Point(5, 8).Point(5, 2).Point(11, 2).MustPolygon(),
				Holes: []geom.Polygon{geom.New().Point(7, 8).Point(7, 3).Point(6, 3).Point(6, 8).MustPolygon()},
			}},
			checkCentroid: true,
		},
		{
			name: "TwoHoles",
			m: geom.MultiPolygon{{
				Outer: geom.New().Point(0, 0).Point(20, 0).Point(20, 10).Point(0, 10).MustPolygon(),
				Holes: []geom.Polygon{
					geom.New().Point(2, 2).Point(2, 6).Point(6, 6).Point(6, 2).MustPolygon(),     // CW
					geom.New().Point(12, 3).Point(12, 7).Point(16, 7).Point(16, 3).MustPolygon(), // CW
				},
			}},
			checkCentroid: true,
		},
		{
			name: "MultiPiece",
			m: geom.MultiPolygon{
				{Outer: geom.New().Point(0, 0).Point(4, 0).Point(4, 4).Point(0, 4).MustPolygon()},
				{Outer: geom.New().Point(10, 10).Point(16, 10).Point(13, 16).MustPolygon()},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want := tc.m.Area()
			if tc.wantArea != nil {
				want = *tc.wantArea
			}
			tris := Triangulate(tc.m)
			require.InDelta(t, want, triSumArea(tris), 1e-9, "area %v, want %v", triSumArea(tris), want)
			for i, tri := range tris {
				if tc.checkCCW {
					require.Greater(t, orient(tri[0], tri[1], tri[2]), 0.0, "triangle %d not CCW: %v", i, tri)
				}
				if tc.checkCentroid {
					c := triCentroid(tri)
					require.True(t, tc.m.Contains(c), "triangle %d centroid %v outside region", i, c)
				}
			}
		})
	}
}

func TestTriangulateDegenerate(t *testing.T) {
	// Fewer than three vertices, or collinear sliver: no triangles.
	for _, m := range []geom.MultiPolygon{
		{{Outer: geom.Polygon{{X: 0, Y: 0}, {X: 1, Y: 1}}}},
		{{Outer: geom.Polygon{{X: 0, Y: 0}}}},
		nil,
		{{Outer: geom.New().Point(0, 0).Point(2, 0).Point(4, 0).MustPolygon()}}, // collinear → zero area
	} {
		tris := Triangulate(m)
		require.Empty(t, tris, "Triangulate(%v) = %d triangles, want 0", m, len(tris))
	}
}

func TestTriangulateCollinearVertices(t *testing.T) {
	// Extra collinear vertices on the edges must not break the result; the
	// covered area stays exact and no zero-area triangles leak out.
	m := geom.MultiPolygon{{Outer: geom.New().
		Point(0, 0).Point(2, 0).Point(4, 0).Point(4, 2).Point(4, 4).Point(2, 4).Point(0, 4).Point(0, 2).
		MustPolygon()}}
	tris := Triangulate(m)
	require.InDelta(t, 16.0, triSumArea(tris), 1e-9, "area %v, want %v", triSumArea(tris), 16.0)
	for i, tri := range tris {
		require.GreaterOrEqual(t, triArea(tri), 1e-12, "triangle %d is degenerate: %v", i, tri)
	}
}

// TestTriangulateAreaOracle is the strong invariant: across many random
// concave polygons (no holes) the summed triangle area must equal the polygon
// area exactly. Overlaps would inflate the sum and gaps would deflate it.
func TestTriangulateAreaOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(20260525))
	for iter := range 20000 {
		ring := randomSimplePolygon(rng)
		if ring == nil || !isSimplePolygon(ring) {
			continue
		}
		m := geom.MultiPolygon{{Outer: ring}}
		want := m.Area()
		if want < 1 {
			continue
		}
		tris := Triangulate(m)
		got := triSumArea(tris)
		require.InDelta(t, want, got, 1e-6*want, "iter %d: area %v, want %v\nring=%v", iter, got, want, ring)
		for i, tri := range tris {
			require.Greater(t, orient(tri[0], tri[1], tri[2]), 0.0, "iter %d: triangle %d not CCW: %v\nring=%v", iter, i, tri, ring)
		}
	}
}

// TestTriangulateBooleanOutput feeds real boolean-engine output through
// Triangulate and checks area conservation, exercising the hole-bridge path on
// engine-produced geometry. The output is normalized through Simplify first —
// the documented pipeline — so holes are strictly interior and rings are not
// self-touching (axis-aligned coincident edges can otherwise leave a hole
// pinched against the outer boundary, which Simplify resolves).
func TestTriangulateBooleanOutput(t *testing.T) {
	rng := rand.New(rand.NewSource(424242))
	for iter := range 2000 {
		a := randomRectMultiPolygon(rng)
		b := randomRectMultiPolygon(rng)
		diff, err := Difference(a, b)
		if err != nil || len(diff) == 0 {
			continue
		}
		res, err := Simplify(diff)
		if err != nil || len(res) == 0 {
			continue
		}
		want := res.Area()
		if want < 1 {
			continue
		}
		tris := Triangulate(res)
		got := triSumArea(tris)
		require.InDelta(t, want, got, 1e-6*math.Max(want, 1), "iter %d: area %v, want %v\nres=%v", iter, got, want, res)
		for i, tri := range tris {
			c := triCentroid(tri)
			require.True(t, res.Contains(c), "iter %d: triangle %d centroid %v outside region", iter, i, c)
		}
	}
}

// TestTriangulateHolesOracle stresses the hole-bridge path: a large outer
// square with up to nine grid-placed rectangular holes, each strictly interior,
// non-touching and non-overlapping (so the input meets Triangulate's
// precondition). The summed triangle area must equal the region area.
func TestTriangulateHolesOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7777))
	for iter := range 5000 {
		ex := geom.ExPolygon{Outer: geom.New().Point(0, 0).Point(90, 0).Point(90, 90).Point(0, 90).MustPolygon()}
		for gx := range 3 {
			for gy := range 3 {
				if rng.Intn(2) == 0 {
					continue
				}
				ox := float64(gx*30) + 3 + rng.Float64()*4
				oy := float64(gy*30) + 3 + rng.Float64()*4
				w := 3 + rng.Float64()*12
				h := 3 + rng.Float64()*12
				// Clockwise hole (library convention).
				ex.Holes = append(ex.Holes, geom.New().
					Point(ox, oy).Point(ox, oy+h).Point(ox+w, oy+h).Point(ox+w, oy).
					MustPolygon())
			}
		}
		m := geom.MultiPolygon{ex}
		want := m.Area()
		tris := Triangulate(m)
		got := triSumArea(tris)
		require.InDelta(t, want, got, 1e-6*want, "iter %d: area %v, want %v (%d holes)", iter, got, want, len(ex.Holes))
		for i, tri := range tris {
			c := triCentroid(tri)
			require.True(t, m.Contains(c), "iter %d: triangle %d centroid %v outside region", iter, i, c)
		}
	}
}

// randomSimplePolygon builds a star-shaped (hence simple) polygon by sampling
// random radii at sorted angles around a centre — a cheap way to get varied
// concave but non-self-intersecting rings.
func randomSimplePolygon(rng *rand.Rand) geom.Polygon {
	n := 3 + rng.Intn(10)
	angles := make([]float64, n)
	for i := range angles {
		angles[i] = rng.Float64() * 2 * math.Pi
	}
	sortFloat(angles)
	b := geom.New()
	count := 0
	for i, ang := range angles {
		if i > 0 && ang-angles[i-1] < 1e-3 {
			continue
		}
		// Vertices sorted by angle around the centre (20,20) form a
		// star-shaped, hence simple, polygon. Do not round: rounding can
		// nudge a vertex across an edge and break simplicity.
		r := 1 + rng.Float64()*9
		b.Point(20+r*math.Cos(ang), 20+r*math.Sin(ang))
		count++
	}
	if count < 3 {
		return nil
	}
	return b.MustPolygon()
}

func randomRectMultiPolygon(rng *rand.Rand) geom.MultiPolygon {
	x0 := float64(rng.Intn(8))
	y0 := float64(rng.Intn(8))
	w := float64(1 + rng.Intn(8))
	h := float64(1 + rng.Intn(8))
	return geom.New().
		Point(x0, y0).Point(x0+w, y0).Point(x0+w, y0+h).Point(x0, y0+h).
		MustBuild()
}

// isSimplePolygon reports whether ring has no pair of non-adjacent edges that
// properly cross — i.e. it is a simple polygon, the precondition Triangulate
// assumes. The random star-shaped generator can violate this when the centre
// falls outside the sampled hull.
func isSimplePolygon(ring geom.Polygon) bool {
	n := len(ring)
	if n < 3 {
		return false
	}
	for i := range n {
		a, b := ring[i], ring[(i+1)%n]
		for j := i + 1; j < n; j++ {
			if j == i || (j+1)%n == i || j == (i+1)%n {
				continue // skip shared-vertex adjacent edges
			}
			if properlyCross(a, b, ring[j], ring[(j+1)%n]) {
				return false
			}
		}
	}
	return true
}

// properlyCross reports a transversal crossing of segments ab and cd at a point
// interior to both (no shared endpoints, no collinear overlap).
func properlyCross(a, b, c, d geom.Point) bool {
	d1 := orient(c, d, a)
	d2 := orient(c, d, b)
	d3 := orient(a, b, c)
	d4 := orient(a, b, d)
	return (d1 > 0) != (d2 > 0) && (d3 > 0) != (d4 > 0) &&
		d1 != 0 && d2 != 0 && d3 != 0 && d4 != 0
}

func sortFloat(s []float64) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
