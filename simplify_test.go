package polyclip

import (
	"math"
	"testing"
)

// TestSimplifyEmpty returns an empty result with no error.
func TestSimplifyEmpty(t *testing.T) {
	got, err := Simplify(nil)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d pieces, want 0", len(got))
	}
}

// TestSimplifySimpleSquareUnchanged passes a simple, already-clean square
// through Simplify and gets back one piece of the same area.
func TestSimplifySimpleSquareUnchanged(t *testing.T) {
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 4, Y: 4}, {X: 0, Y: 4},
	}}}
	got, err := Simplify(in)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pieces, want 1", len(got))
	}
	if a := got.Area(); math.Abs(a-16) > 1e-6 {
		t.Errorf("area %.6f, want 16", a)
	}
	if iss := got.Validate(); len(iss) != 0 {
		t.Errorf("output not clean: %v", iss)
	}
}

// TestSimplifyBowtieSplits resolves a self-crossing bowtie into its two
// oppositely-wound triangles. Under the non-zero rule both lobes (|winding|==1)
// are filled, so the result is two triangles meeting at the crossing point.
func TestSimplifyBowtieSplits(t *testing.T) {
	// Vertices traversed 0→1→2→3: the two diagonals cross at (2,2).
	in := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 4, Y: 0}, {X: 0, Y: 4}, {X: 4, Y: 4},
	}}}
	got, err := Simplify(in)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d pieces, want 2 triangles", len(got))
	}
	// Each triangle: base 4, height 2 → area 4; total 8.
	if a := got.Area(); math.Abs(a-8) > 1e-6 {
		t.Errorf("total area %.6f, want 8", a)
	}
	if iss := got.Validate(); len(iss) != 0 {
		t.Errorf("output not clean: %v", iss)
	}
}

// TestSimplifyResolvesSelfIntersecting is the motivating case: a
// self-intersecting ring (which Validate flags) is cleaned into a valid
// (non-self-intersecting) shape — something running Union of the input with
// itself cannot do (the idempotency short-circuit leaves it unchanged).
func TestSimplifyResolvesSelfIntersecting(t *testing.T) {
	// A self-intersecting arrowhead whose strokes cross each other.
	star := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 6}, {X: 0, Y: 4}, {X: 10, Y: 0}, {X: 0, Y: 6},
	}}}
	if iss := star.Validate(); len(iss) == 0 {
		t.Fatalf("test setup: input should be self-intersecting")
	}

	// Union with itself short-circuits and leaves it dirty (unchanged).
	self, err := Union(star, star)
	if err != nil {
		t.Fatalf("Union err %v", err)
	}
	if !mpolyEqual(self, star) {
		t.Logf("note: Union(A,A) did not return the input verbatim for this case")
	}

	got, err := Simplify(star)
	if err != nil {
		t.Fatalf("Simplify err %v", err)
	}
	if iss := got.Validate(); len(iss) != 0 {
		t.Errorf("Simplify output not clean: %v", iss)
	}
	if got.Area() <= 0 {
		t.Errorf("Simplify produced empty/zero-area result")
	}
}
