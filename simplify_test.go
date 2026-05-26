package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// TestSimplifyEmpty returns an empty result with no error.
func TestSimplifyEmpty(t *testing.T) {
	got, err := Simplify(nil)
	require.NoError(t, err)
	require.Empty(t, got, "got %d pieces, want 0", len(got))
}

// TestSimplifySimpleSquareUnchanged passes a simple, already-clean square
// through Simplify and gets back one piece of the same area.
func TestSimplifySimpleSquareUnchanged(t *testing.T) {
	in := geom.New().Point(0, 0).Point(4, 0).Point(4, 4).Point(0, 4).MustBuild()
	got, err := Simplify(in)
	require.NoError(t, err)
	require.Len(t, got, 1, "got %d pieces, want 1", len(got))
	require.InDelta(t, 16, got.Area(), 1e-6, "area %.6f, want 16", got.Area())
	require.Empty(t, got.Validate(), "output not clean: %v", got.Validate())
}

// TestSimplifyBowtieSplits resolves a self-crossing bowtie into its two
// oppositely-wound triangles. Under the non-zero rule both lobes (|winding|==1)
// are filled, so the result is two triangles meeting at the crossing point.
func TestSimplifyBowtieSplits(t *testing.T) {
	// Vertices traversed 0→1→2→3: the two diagonals cross at (2,2).
	in := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(4, 0).Point(0, 4).Point(4, 4).
		MustPolygon()}}
	got, err := Simplify(in)
	require.NoError(t, err)
	require.Len(t, got, 2, "got %d pieces, want 2 triangles", len(got))
	// Each triangle: base 4, height 2 → area 4; total 8.
	require.InDelta(t, 8, got.Area(), 1e-6, "total area %.6f, want 8", got.Area())
	require.Empty(t, got.Validate(), "output not clean: %v", got.Validate())
}

// TestSimplifyResolvesSelfIntersecting is the motivating case: a
// self-intersecting ring (which Validate flags) is cleaned into a valid
// (non-self-intersecting) shape — something running Union of the input with
// itself cannot do (the idempotency short-circuit leaves it unchanged).
func TestSimplifyResolvesSelfIntersecting(t *testing.T) {
	// A self-intersecting arrowhead whose strokes cross each other.
	star := geom.MultiPolygon{geom.ExPolygon{Outer: geom.New().
		Point(0, 0).Point(10, 6).Point(0, 4).Point(10, 0).Point(0, 6).
		MustPolygon()}}
	require.NotEmpty(t, star.Validate(), "test setup: input should be self-intersecting")

	// Union with itself short-circuits and leaves it dirty (unchanged).
	self, err := Union(star, star)
	require.NoError(t, err)
	if !mpolyEqual(self, star) {
		t.Logf("note: Union(A,A) did not return the input verbatim for this case")
	}

	got, err := Simplify(star)
	require.NoError(t, err)
	require.Empty(t, got.Validate(), "Simplify output not clean: %v", got.Validate())
	require.True(t, got.Area() > 0, "Simplify produced empty/zero-area result")
}
