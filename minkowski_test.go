package polyclip

import (
	"math"
	"testing"
)

// square2 is a 2x2 axis-aligned pattern anchored at the origin corner.
var square2 = Polygon{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}

// TestMinkowskiSumOpenSegment sweeps the pattern along a single horizontal
// open segment: the 2x2 square translated from (0,0) to (10,0) fills the
// rectangle [0,12]x[0,2].
func TestMinkowskiSumOpenSegment(t *testing.T) {
	got, err := MinkowskiSum(square2, []Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, false)
	if err != nil {
		t.Fatalf("MinkowskiSum: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pieces, want 1", len(got))
	}
	if a := got.Area(); math.Abs(a-24) > 1e-9 {
		t.Errorf("area = %v, want 24", a)
	}
	b := bboxOf([]Polygon{got[0].Outer})
	if b.Min != (Point{X: 0, Y: 0}) || b.Max != (Point{X: 12, Y: 2}) {
		t.Errorf("bbox = %+v, want [0,0]-[12,2]", b)
	}
}

// TestMinkowskiDiffOpenSegment reflects the pattern through the origin, so the
// same segment sweep fills [-2,10]x[-2,0] (area 24).
func TestMinkowskiDiffOpenSegment(t *testing.T) {
	got, err := MinkowskiDiff(square2, []Point{{X: 0, Y: 0}, {X: 10, Y: 0}}, false)
	if err != nil {
		t.Fatalf("MinkowskiDiff: %v", err)
	}
	if a := got.Area(); math.Abs(a-24) > 1e-9 {
		t.Errorf("area = %v, want 24", a)
	}
	b := bboxOf([]Polygon{got[0].Outer})
	if b.Min != (Point{X: -2, Y: -2}) || b.Max != (Point{X: 10, Y: 0}) {
		t.Errorf("bbox = %+v, want [-2,-2]-[10,0]", b)
	}
}

// TestMinkowskiSumClosedSquareFrame sweeps the pattern around a closed 10x10
// square boundary. The boundary band is one ExPolygon with a single hole: the
// outer extent is [0,12]x[0,12] (area 144) and the uncovered interior is the
// [2,10]x[2,10] hole (area 64), leaving a frame of area 80.
func TestMinkowskiSumClosedSquareFrame(t *testing.T) {
	path := []Point{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	got, err := MinkowskiSum(square2, path, true)
	if err != nil {
		t.Fatalf("MinkowskiSum: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d pieces, want 1", len(got))
	}
	if n := len(got[0].Holes); n != 1 {
		t.Fatalf("got %d holes, want 1", n)
	}
	if a := got.Area(); math.Abs(a-80) > 1e-9 {
		t.Errorf("area = %v, want 80", a)
	}
	if a := got[0].Outer.Area(); math.Abs(a-144) > 1e-9 {
		t.Errorf("outer area = %v, want 144", a)
	}
	if a := got[0].Holes[0].Area(); math.Abs(a-64) > 1e-9 {
		t.Errorf("hole area = %v, want 64", a)
	}
}

// TestMinkowskiEmptyInputs returns an empty result for an empty pattern or path.
func TestMinkowskiEmptyInputs(t *testing.T) {
	seg := []Point{{X: 0, Y: 0}, {X: 1, Y: 0}}
	if got, err := MinkowskiSum(Polygon{}, seg, false); err != nil || len(got) != 0 {
		t.Errorf("empty pattern: got %v, %v; want empty, nil", got, err)
	}
	if got, err := MinkowskiSum(square2, nil, false); err != nil || len(got) != 0 {
		t.Errorf("empty path: got %v, %v; want empty, nil", got, err)
	}
}
