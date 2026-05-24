package polyclip

import "testing"

// TestHorizJoinHangRepro is the minimal repro for the processHorzJoins
// infinite loop found by the §7.5 reachability harness: Difference of two
// axis-aligned skyline polygons spins forever in the horizontal-join merge.
func TestHorizJoinHangRepro(t *testing.T) {
	a := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 7, Y: 0}, {X: 7, Y: 6}, {X: 6, Y: 6}, {X: 5, Y: 6},
		{X: 5, Y: 2}, {X: 4, Y: 2}, {X: 3, Y: 2}, {X: 3, Y: 4}, {X: 2, Y: 4},
		{X: 2, Y: 6}, {X: 1, Y: 6}, {X: 1, Y: 3}, {X: 0, Y: 3},
	}}}
	b := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 1, Y: 1}, {X: 4, Y: 1}, {X: 4, Y: 2}, {X: 3, Y: 2},
		{X: 3, Y: 4}, {X: 2, Y: 4}, {X: 1, Y: 4},
	}}}
	if errs := a.Validate(); len(errs) != 0 {
		t.Fatalf("A invalid: %v", errs)
	}
	if errs := b.Validate(); len(errs) != 0 {
		t.Fatalf("B invalid: %v", errs)
	}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("Difference err: %v", err)
	}
	t.Logf("Difference area=%v result=%v", got.Area(), got)
}
