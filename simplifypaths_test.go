package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// TestSimplifyPathsRemovesCollinear drops vertices that lie exactly on the line
// through their neighbours, leaving the shape (and its area) unchanged.
func TestSimplifyPathsRemovesCollinear(t *testing.T) {
	// A unit-area-100 square with two redundant collinear midpoints on its
	// bottom and right edges.
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(5, 0).Point(10, 0).
		Point(10, 5).Point(10, 10).Point(0, 10).
		MustPolygon()}}
	got := SimplifyPaths(in, 0.001)
	require.Len(t, got, 1, "got %d pieces, want 1", len(got))
	require.Len(t, got[0].Outer, 4, "outer has %d vertices, want 4: %+v", len(got[0].Outer), got[0].Outer)
	require.InDelta(t, 100, got.Area(), 1e-9, "area %.9f, want 100", got.Area())
}

// TestSimplifyPathsEpsilonThreshold keeps a vertex whose deviation exceeds
// epsilon and removes one whose deviation is within it.
func TestSimplifyPathsEpsilonThreshold(t *testing.T) {
	// Bottom edge has a bump at (5, 0.4): perpendicular distance 0.4 from the
	// (0,0)-(10,0) line. eps=0.5 removes it; eps=0.3 keeps it.
	mk := func() geom.MultiPolygon {
		return geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
			Point(0, 0).Point(5, 0.4).Point(10, 0).
			Point(10, 10).Point(0, 10).
			MustPolygon()}}
	}
	removed := SimplifyPaths(mk(), 0.5)
	require.Len(t, removed[0].Outer, 4, "eps=0.5: %d vertices, want 4 (bump removed)", len(removed[0].Outer))
	kept := SimplifyPaths(mk(), 0.3)
	require.Len(t, kept[0].Outer, 5, "eps=0.3: %d vertices, want 5 (bump kept)", len(kept[0].Outer))
}

// TestSimplifyPathsSimplifiesHoles applies reduction to hole rings too.
func TestSimplifyPathsSimplifiesHoles(t *testing.T) {
	in := geom.MultiPolygon{geom.ExPolygon{
		Outer: geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(),
		Holes: []geom.Polygon{geom.New().
			Point(2, 2).Point(4, 2).Point(6, 2). // (4,2) collinear
			Point(6, 6).Point(2, 6).
			MustPolygon()},
	}}
	got := SimplifyPaths(in, 0.001)
	require.True(t, len(got) == 1 && len(got[0].Holes) == 1, "got %d pieces / %d holes, want 1/1", len(got), len(got[0].Holes))
	require.Len(t, got[0].Holes[0], 4, "hole has %d vertices, want 4", len(got[0].Holes[0]))
}

// TestSimplifyPathsKeepsSmallRings leaves rings of fewer than four vertices
// untouched — no interior vertex can be removed without degenerating them.
func TestSimplifyPathsKeepsSmallRings(t *testing.T) {
	tri := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(4, 0).Point(2, 3).
		MustPolygon()}}
	got := SimplifyPaths(tri, 100) // huge epsilon
	require.True(t, len(got) == 1 && len(got[0].Outer) == 3, "triangle changed: %+v", got)
}

// TestSimplifyPathsDropsDegenerateRing drops an ExPolygon whose outer ring
// collapses below three vertices (a near-degenerate sliver under a large eps).
func TestSimplifyPathsDropsDegenerateRing(t *testing.T) {
	// Four near-collinear points forming a thin sliver; a large epsilon
	// removes the interior pair, leaving < 3 vertices.
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(3, 0.01).Point(6, 0).Point(3, -0.01).
		MustPolygon()}}
	got := SimplifyPaths(in, 1.0)
	require.Empty(t, got, "got %d pieces, want 0 (sliver dropped)", len(got))
}

// TestSimplifyPathsNegativeEpsilon treats a negative epsilon as zero: only
// exactly-collinear vertices are removed.
func TestSimplifyPathsNegativeEpsilon(t *testing.T) {
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(5, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		MustPolygon()}}
	got := SimplifyPaths(in, -5)
	require.Len(t, got[0].Outer, 4, "got %d vertices, want 4 (exact collinear removed)", len(got[0].Outer))
}
