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
	// One diamond as subject only — should produce one closed ring.
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
