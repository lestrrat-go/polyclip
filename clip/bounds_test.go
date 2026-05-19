package clip

import (
	"errors"
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

func TestBuildLocalMinimaAxialRectangle(t *testing.T) {
	// CCW axial rectangle: one local min at the bottom-left, one local max
	// at the top-right. Each bound has two segments — one horizontal and
	// one vertical.
	segs := axialRect(0, 0, 10, 5, Subject)
	minima, err := BuildLocalMinima(segs)
	if err != nil {
		t.Fatalf("BuildLocalMinima err: %v", err)
	}
	if len(minima) != 1 {
		t.Fatalf("expected 1 local min, got %d", len(minima))
	}
	m := minima[0]
	if m.Vertex != (fixed.Point{X: 0, Y: 0}) {
		t.Errorf("vertex: %v want (0,0)", m.Vertex)
	}
	if m.Left == nil || m.Right == nil {
		t.Fatalf("bounds nil: left=%v right=%v", m.Left, m.Right)
	}
	if len(m.Left.Segs) != 2 {
		t.Errorf("left segs: %d want 2; segs=%v", len(m.Left.Segs), m.Left.Segs)
	}
	if len(m.Right.Segs) != 2 {
		t.Errorf("right segs: %d want 2; segs=%v", len(m.Right.Segs), m.Right.Segs)
	}

	// Left bound's first non-horizontal should be at X=0 (left vertical).
	// Right bound's first non-horizontal should be at X=10 (right vertical).
	if x := boundInitialX(m.Left, m.Vertex); x != 0 {
		t.Errorf("left bound initial X: %d want 0", int64(x))
	}
	if x := boundInitialX(m.Right, m.Vertex); x != 10 {
		t.Errorf("right bound initial X: %d want 10", int64(x))
	}

	// Both bounds must end at the local max (1 of the two top vertices,
	// whichever the down→up transition selected). For axialRect it's the
	// end vertex of the up edge (1,0)→(1,5), which is (10,5).
	leftLast := m.Left.Last()
	rightLast := m.Right.Last()
	if topY := fixed.Coord(5); leftLast.Top.Y != topY || rightLast.Top.Y != topY {
		t.Errorf("bounds last edge top Y: left=%v right=%v want both at Y=%d",
			leftLast.Top, rightLast.Top, int64(topY))
	}
}

func TestBuildLocalMinimaDiamond(t *testing.T) {
	// CCW diamond: one local min at the bottom vertex, one local max at
	// the top. Each bound has two non-horizontal segments.
	segs := diamond(0, 0, 10, Subject)
	minima, err := BuildLocalMinima(segs)
	if err != nil {
		t.Fatalf("BuildLocalMinima err: %v", err)
	}
	if len(minima) != 1 {
		t.Fatalf("expected 1 local min, got %d", len(minima))
	}
	m := minima[0]
	if m.Vertex != (fixed.Point{X: 0, Y: -10}) {
		t.Errorf("vertex: %v want (0,-10)", m.Vertex)
	}
	if len(m.Left.Segs) != 2 {
		t.Errorf("left segs: %d want 2", len(m.Left.Segs))
	}
	if len(m.Right.Segs) != 2 {
		t.Errorf("right segs: %d want 2", len(m.Right.Segs))
	}
	// Left bound's first segment goes UP-LEFT from local min: (0,-10) →
	// (-10,0). Slope -1.
	// Right bound's first goes UP-RIGHT: (0,-10) → (10,0). Slope +1.
	if s := boundInitialSlope(m.Left); s >= 0 {
		t.Errorf("left bound initial slope: %v want negative", s)
	}
	if s := boundInitialSlope(m.Right); s <= 0 {
		t.Errorf("right bound initial slope: %v want positive", s)
	}
}

func TestBuildLocalMinimaTwoDisjointRectangles(t *testing.T) {
	// Two CCW axial rectangles, no shared vertices. Two local minima
	// expected, sorted by (Y, X).
	var segs []Segment
	segs = append(segs, axialRect(0, 0, 5, 3, Subject)...)
	segs = append(segs, axialRect(20, 10, 25, 13, Clip)...)
	minima, err := BuildLocalMinima(segs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(minima) != 2 {
		t.Fatalf("expected 2 local minima, got %d", len(minima))
	}
	// Sorted: (0,0) first (Y=0), then (20,10).
	if minima[0].Vertex != (fixed.Point{X: 0, Y: 0}) {
		t.Errorf("first min vertex: %v want (0,0)", minima[0].Vertex)
	}
	if minima[1].Vertex != (fixed.Point{X: 20, Y: 10}) {
		t.Errorf("second min vertex: %v want (20,10)", minima[1].Vertex)
	}
}

func TestBuildLocalMinimaNestedRectangles(t *testing.T) {
	// Outer CCW axial rectangle + inner CCW axial rectangle (NOT a hole;
	// just two independent rings as far as BuildLocalMinima is concerned).
	// Two local minima — both at the bottom-left of their respective
	// rectangles.
	var segs []Segment
	segs = append(segs, axialRect(0, 0, 20, 20, Subject)...)
	segs = append(segs, axialRect(5, 5, 15, 15, Subject)...)
	minima, err := BuildLocalMinima(segs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(minima) != 2 {
		t.Fatalf("expected 2 local minima, got %d", len(minima))
	}
	if minima[0].Vertex != (fixed.Point{X: 0, Y: 0}) {
		t.Errorf("first min: %v", minima[0].Vertex)
	}
	if minima[1].Vertex != (fixed.Point{X: 5, Y: 5}) {
		t.Errorf("second min: %v", minima[1].Vertex)
	}
}

func TestBuildLocalMinimaSharedVertexErrors(t *testing.T) {
	// Two squares sharing a corner — byStart map collision should be
	// detected and return ErrOpenRing.
	var segs []Segment
	segs = append(segs, axialRect(0, 0, 5, 5, Subject)...)
	segs = append(segs, axialRect(5, 5, 10, 10, Clip)...)
	_, err := BuildLocalMinima(segs)
	if !errors.Is(err, ErrOpenRing) {
		t.Fatalf("expected ErrOpenRing, got %v", err)
	}
}

func TestBuildLocalMinimaEmpty(t *testing.T) {
	minima, err := BuildLocalMinima(nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(minima) != 0 {
		t.Errorf("expected 0 minima, got %d", len(minima))
	}
}

func TestBuildLocalMinimaWShape(t *testing.T) {
	// CCW "W"-like polygon with TWO local minima (bottom-left of the W and
	// bottom-right) and ONE local maximum (top of the W, at the central
	// dip-up). Vertices (sloped, no horizontals):
	//   v0(0,0) — bottom-left local min
	//   v1(5,5) — central peak (local max for the left half, but...)
	//   v2(10,0) — bottom-right local min
	//   v3(10,20) — top-right corner (true local max)
	//   v4(0,20) — top-left corner
	// Walking CCW: v0 → v2 → v3 → v4 → v1 → v0? No, let me reconsider.
	//
	// Simpler W: two valleys joined by a peak.
	//   v0(0,10) — top-left
	//   v1(2,0)  — bottom-left local min
	//   v2(5,8)  — middle peak (local max)
	//   v3(8,0)  — bottom-right local min
	//   v4(10,10) — top-right
	// CCW: v4 → v3 → v2 → v1 → v0 → v4. (Going right-to-left along the top,
	// then down-up-down-up zigzag along the bottom.)
	pts := []fixed.Point{
		{X: 10, Y: 10}, // v4 top-right
		{X: 8, Y: 0},   // v3
		{X: 5, Y: 8},   // v2 peak
		{X: 2, Y: 0},   // v1
		{X: 0, Y: 10},  // v0 top-left
	}
	n := len(pts)
	segs := make([]Segment, 0, n)
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		seg := NewSegment(pts[i], pts[j], Subject)
		if !seg.Degenerate() {
			segs = append(segs, seg)
		}
	}
	minima, err := BuildLocalMinima(segs)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(minima) != 2 {
		t.Fatalf("expected 2 local minima, got %d; minima=%v", len(minima), minima)
	}
	// Sorted by (Y, X): both at Y=0, X=2 then X=8.
	if minima[0].Vertex != (fixed.Point{X: 2, Y: 0}) {
		t.Errorf("first min: %v want (2,0)", minima[0].Vertex)
	}
	if minima[1].Vertex != (fixed.Point{X: 8, Y: 0}) {
		t.Errorf("second min: %v want (8,0)", minima[1].Vertex)
	}
}
