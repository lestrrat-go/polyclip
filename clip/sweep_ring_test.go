package clip

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

// diamond returns the four segments of a CCW unit-ish diamond centred at
// (cx, cy) with half-diagonal r. All Ys are distinct so the sweep sees
// proper local min / max / through events.
func diamond(cx, cy, r int64, src Source) []Segment {
	v0 := fixed.Point{X: fixed.Coord(cx), Y: fixed.Coord(cy - r)}
	v1 := fixed.Point{X: fixed.Coord(cx + r), Y: fixed.Coord(cy)}
	v2 := fixed.Point{X: fixed.Coord(cx), Y: fixed.Coord(cy + r)}
	v3 := fixed.Point{X: fixed.Coord(cx - r), Y: fixed.Coord(cy)}
	return []Segment{
		NewSegment(v0, v1, src),
		NewSegment(v1, v2, src),
		NewSegment(v2, v3, src),
		NewSegment(v3, v0, src),
	}
}

func TestSweepDiamondSubject(t *testing.T) {
	// One diamond as subject only — should produce one closed ring in
	// CCW Next-direction (positive signed area). With the bound-model
	// pre-pass providing deterministic orientation, the result is no
	// longer heap-dependent.
	segs := diamond(0, 0, 10, Subject)
	r := Sweep(segs, OpUnion)

	closed := closedRings(r.Rings)
	if len(closed) != 1 {
		t.Fatalf("closed ring count: %d want 1; rings=%+v", len(closed), summarizeRings(r.Rings))
	}
	pts := closed[0].Points()
	if len(pts) != 4 {
		t.Errorf("ring vertex count: %d want 4; pts=%v", len(pts), pts)
	}
	if a := signedArea(pts); a <= 0 {
		t.Errorf("ring traverses CW (signed area %d, want positive — CCW); pts=%v", a, pts)
	}
	// All four diamond vertices should appear.
	want := map[fixed.Point]bool{
		{X: 0, Y: -10}: true,
		{X: 10, Y: 0}:  true,
		{X: 0, Y: 10}:  true,
		{X: -10, Y: 0}: true,
	}
	for _, p := range pts {
		if !want[p] {
			t.Errorf("unexpected vertex %v in ring", p)
		}
		delete(want, p)
	}
	if len(want) > 0 {
		t.Errorf("missing vertices: %v", want)
	}
}

func TestSweepTwoDisjointDiamonds(t *testing.T) {
	// Two diamonds far apart — should produce two independent rings.
	var segs []Segment
	segs = append(segs, diamond(0, 0, 10, Subject)...)
	segs = append(segs, diamond(100, 100, 10, Clip)...)
	r := Sweep(segs, OpUnion)

	closed := closedRings(r.Rings)
	if len(closed) != 2 {
		t.Fatalf("closed ring count: %d want 2", len(closed))
	}
	for _, ring := range closed {
		if len(ring.Points()) != 4 {
			t.Errorf("diamond ring should have 4 vertices, got %d", len(ring.Points()))
		}
	}
}

func closedRings(rings []*OutRec) []*OutRec {
	var out []*OutRec
	for _, r := range rings {
		if r.Pts != nil {
			out = append(out, r)
		}
	}
	return out
}

func summarizeRings(rings []*OutRec) []string {
	out := make([]string, len(rings))
	for i, r := range rings {
		if r.Pts == nil {
			out[i] = "<merged>"
			continue
		}
		out[i] = formatRing(r.Points())
	}
	return out
}

func formatRing(pts []fixed.Point) string {
	parts := make([]string, len(pts))
	for i, p := range pts {
		parts[i] = fmt.Sprintf("(%d,%d)", int64(p.X), int64(p.Y))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

// axialRect returns the four segments of a CCW axis-aligned rectangle —
// includes two horizontals (bottom and top) and two verticals. This is the
// minimum input that exercises the EventHoriz / EventHorizMaxOpen handlers.
func axialRect(x0, y0, x1, y1 int64, src Source) []Segment {
	v0 := fixed.Point{X: fixed.Coord(x0), Y: fixed.Coord(y0)}
	v1 := fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y0)}
	v2 := fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y1)}
	v3 := fixed.Point{X: fixed.Coord(x0), Y: fixed.Coord(y1)}
	return []Segment{
		NewSegment(v0, v1, src),
		NewSegment(v1, v2, src),
		NewSegment(v2, v3, src),
		NewSegment(v3, v0, src),
	}
}

func signedArea(pts []fixed.Point) int64 {
	if len(pts) < 3 {
		return 0
	}
	var s int64
	n := len(pts)
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		s += int64(pts[i].X)*int64(pts[j].Y) - int64(pts[j].X)*int64(pts[i].Y)
	}
	return s
}

func TestSweepAxialRectangleSubject(t *testing.T) {
	// One CCW axial rectangle as subject only. Must produce a single closed
	// ring of 4 vertices in CCW order (positive signed area).
	segs := axialRect(0, 0, 10, 5, Subject)
	r := Sweep(segs, OpUnion)
	if r.Err != nil {
		t.Fatalf("sweep err: %v", r.Err)
	}

	closed := closedRings(r.Rings)
	if len(closed) != 1 {
		t.Fatalf("closed ring count: %d want 1; rings=%v", len(closed), summarizeRings(r.Rings))
	}
	pts := closed[0].Points()
	if len(pts) != 4 {
		t.Errorf("vertex count: %d want 4; pts=%v", len(pts), pts)
	}

	want := map[fixed.Point]bool{
		{X: 0, Y: 0}:  true,
		{X: 10, Y: 0}: true,
		{X: 10, Y: 5}: true,
		{X: 0, Y: 5}:  true,
	}
	for _, p := range pts {
		if !want[p] {
			t.Errorf("unexpected vertex %v in ring", p)
		}
		delete(want, p)
	}
	if len(want) > 0 {
		t.Errorf("missing vertices: %v", want)
	}

	// Signed area: 2*Area of unit rectangle = 2*50 = 100 (CCW positive).
	if a := signedArea(pts); a <= 0 {
		t.Errorf("ring traverses CW (signed area %d, want positive); pts=%v", a, pts)
	}
}

func TestSweepStaircasePolygon(t *testing.T) {
	// L-shaped staircase polygon with a mid-bound horizontal (e2 = (2,2)→(4,2))
	// inside the Right bound from the local minimum at (0,0). Per DESIGN.md
	// §12.10.5's worked trace, this exercises advanceBoundCursor's mid-bound
	// horizontal emission and the trailing-horizontal close path.
	pts := []fixed.Point{
		{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2},
		{X: 4, Y: 2}, {X: 4, Y: 4}, {X: 0, Y: 4},
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
	r := Sweep(segs, OpUnion)
	if r.Err != nil {
		t.Fatalf("sweep err: %v", r.Err)
	}
	closed := closedRings(r.Rings)
	if len(closed) != 1 {
		t.Fatalf("closed ring count: %d want 1; rings=%v", len(closed), summarizeRings(r.Rings))
	}
	ringPts := closed[0].Points()
	if len(ringPts) != 6 {
		t.Errorf("vertex count: %d want 6; pts=%v", len(ringPts), ringPts)
	}
	if a := signedArea(ringPts); a <= 0 {
		t.Errorf("ring traverses CW (signed area %d, want positive — CCW); pts=%v", a, ringPts)
	}
	// All six input vertices should appear in the output ring.
	want := map[fixed.Point]bool{
		{X: 0, Y: 0}: true, {X: 2, Y: 0}: true, {X: 2, Y: 2}: true,
		{X: 4, Y: 2}: true, {X: 4, Y: 4}: true, {X: 0, Y: 4}: true,
	}
	for _, p := range ringPts {
		if !want[p] {
			t.Errorf("unexpected vertex %v", p)
		}
		delete(want, p)
	}
	if len(want) > 0 {
		t.Errorf("missing vertices: %v", want)
	}
}

func TestSweepWShapePolygon(t *testing.T) {
	// CCW "W" polygon with two local minima — exercises handleLocalMinimum
	// with the bound pre-pass to ensure each minimum gets the correct
	// Right/Left bound orientation regardless of heap order.
	pts := []fixed.Point{
		{X: 10, Y: 10}, // top-right
		{X: 8, Y: 0},   // bottom-right local min
		{X: 5, Y: 8},   // middle peak (local max)
		{X: 2, Y: 0},   // bottom-left local min
		{X: 0, Y: 10},  // top-left
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
	r := Sweep(segs, OpUnion)
	if r.Err != nil {
		t.Fatalf("sweep err: %v", r.Err)
	}
	closed := closedRings(r.Rings)
	if len(closed) != 1 {
		t.Fatalf("closed ring count: %d want 1; rings=%v", len(closed), summarizeRings(r.Rings))
	}
	ringPts := closed[0].Points()
	if len(ringPts) != 5 {
		t.Errorf("vertex count: %d want 5; pts=%v", len(ringPts), ringPts)
	}
	if a := signedArea(ringPts); a <= 0 {
		t.Errorf("ring traverses CW (signed area %d, want positive — CCW); pts=%v", a, ringPts)
	}
}

func TestSweepTwoDisjointAxialRectangles(t *testing.T) {
	// Two CCW axial rectangles, far apart. Bboxes don't intersect so the
	// engine sees both independently.
	var segs []Segment
	segs = append(segs, axialRect(0, 0, 10, 5, Subject)...)
	segs = append(segs, axialRect(100, 100, 110, 105, Clip)...)
	r := Sweep(segs, OpUnion)
	if r.Err != nil {
		t.Fatalf("sweep err: %v", r.Err)
	}
	closed := closedRings(r.Rings)
	if len(closed) != 2 {
		t.Fatalf("closed ring count: %d want 2; rings=%v", len(closed), summarizeRings(r.Rings))
	}
	for _, ring := range closed {
		pts := ring.Points()
		if len(pts) != 4 {
			t.Errorf("vertex count: %d want 4", len(pts))
		}
		if a := signedArea(pts); a <= 0 {
			t.Errorf("ring traverses CW (signed area %d, want positive); pts=%v", a, pts)
		}
	}
}
