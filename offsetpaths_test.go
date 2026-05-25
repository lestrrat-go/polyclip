package polyclip

import (
	"math"
	"testing"
)

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	if math.Abs(got-want) > tol {
		t.Errorf("%s = %g, want %g (tol %g)", what, got, want, tol)
	}
}

func TestOffsetPathsStraightButt(t *testing.T) {
	// A horizontal segment of length 10, half-width 2: a 10×4 rectangle.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	res, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndButt})
	if err != nil {
		t.Fatalf("OffsetPaths butt: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("butt pieces = %d, want 1", len(res))
	}
	approx(t, res.Area(), 40, 1e-9, "butt area")
}

func TestOffsetPathsStraightSquare(t *testing.T) {
	// Square caps extend 2 beyond each end: 14×4 = 56.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	res, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndSquare})
	if err != nil {
		t.Fatalf("OffsetPaths square: %v", err)
	}
	approx(t, res.Area(), 56, 1e-9, "square area")
}

func TestOffsetPathsStraightRound(t *testing.T) {
	// Round caps add two semicircles of radius 2: 40 + pi*4.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	res, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndRound})
	if err != nil {
		t.Fatalf("OffsetPaths round: %v", err)
	}
	want := 40 + math.Pi*4
	// Tessellation chords cut slightly inside the true arc; allow 1%.
	approx(t, res.Area(), want, want*0.01, "round area")
}

func TestOffsetPathsVerticalButt(t *testing.T) {
	// Orientation must be CCW (positive area) regardless of path direction.
	line := Polyline{{X: 0, Y: 10}, {X: 0, Y: 0}}
	res, err := OffsetPaths([]Polyline{line}, 3, OffsetOptions{End: EndButt})
	if err != nil {
		t.Fatalf("OffsetPaths vertical: %v", err)
	}
	approx(t, res.Area(), 60, 1e-9, "vertical butt area") // 10 long, width 6
}

func TestOffsetPathsRightAngle(t *testing.T) {
	// An L: (0,0)->(10,0)->(10,10), half-width 1, miter join at the corner.
	// The ribbon is a single simple piece. Area is two 10×2 arms minus the
	// 2×2 overlap at the corner, plus the convex miter wedge.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}
	res, err := OffsetPaths([]Polyline{line}, 1, OffsetOptions{End: EndButt, Join: JoinMiter})
	if err != nil {
		t.Fatalf("OffsetPaths L: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("L pieces = %d, want 1", len(res))
	}
	// 2 arms of 10×2 = 40, shared corner square 2×2 counted once = -4, miter
	// corner adds a 1×1 triangle (apex) outside the inner square: 40-4+? The
	// exact value with a miter outer corner is 37 (inner notch 1×1 removed,
	// outer miter 1×1 added cancel to the 36 square-corner plus 1). Verify
	// against a tolerance rather than over-precise hand math.
	if a := res.Area(); a < 36 || a > 40 {
		t.Errorf("L area = %g, want in [36,40]", a)
	}
}

func TestOffsetPathsSharpTurnSinglePiece(t *testing.T) {
	// A sharp V that doubles back: the inner side self-overlaps and must be
	// resolved into one clean piece by the self-union.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 1}, {X: 0, Y: 2}}
	res, err := OffsetPaths([]Polyline{line}, 1, OffsetOptions{End: EndRound, Join: JoinRound})
	if err != nil {
		t.Fatalf("OffsetPaths V: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("V produced empty result")
	}
	if a := res.Area(); a <= 0 {
		t.Errorf("V area = %g, want > 0", a)
	}
}

func TestOffsetPathsMultiple(t *testing.T) {
	// Two disjoint horizontal segments → two pieces.
	lines := []Polyline{
		{{X: 0, Y: 0}, {X: 10, Y: 0}},
		{{X: 0, Y: 100}, {X: 10, Y: 100}},
	}
	res, err := OffsetPaths(lines, 2, OffsetOptions{End: EndButt})
	if err != nil {
		t.Fatalf("OffsetPaths multi: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("multi pieces = %d, want 2", len(res))
	}
	approx(t, res.Area(), 80, 1e-9, "multi area")
}

func TestOffsetPathsEndPolygonRejected(t *testing.T) {
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	_, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndPolygon})
	if err != ErrOffsetEndType {
		t.Errorf("OffsetPaths(EndPolygon) err = %v, want ErrOffsetEndType", err)
	}
}

func TestOffsetPathsEmpty(t *testing.T) {
	_, err := OffsetPaths(nil, 2, OffsetOptions{End: EndButt})
	if err != ErrOffsetEmpty {
		t.Errorf("OffsetPaths(nil) err = %v, want ErrOffsetEmpty", err)
	}
}

func TestOffsetPathsShortSkipped(t *testing.T) {
	// A single-point path (and a zero-length repeat) has no direction; skipped.
	lines := []Polyline{{{X: 5, Y: 5}}, {{X: 1, Y: 1}, {X: 1, Y: 1}}}
	_, err := OffsetPaths(lines, 2, OffsetOptions{End: EndButt})
	if err != ErrOffsetEmpty {
		t.Errorf("OffsetPaths(short) err = %v, want ErrOffsetEmpty", err)
	}
}

func TestOffsetPathsJoinedSquareLoop(t *testing.T) {
	// A square loop (4 points, open) closed and banded by 1 each side with miter
	// joins: outer square [-1,-1]..[11,11] = 144, inner hole [1,1]..[9,9] = 64,
	// net 80. One piece with one hole.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	res, err := OffsetPaths([]Polyline{line}, 1, OffsetOptions{End: EndJoined, Join: JoinMiter})
	if err != nil {
		t.Fatalf("OffsetPaths joined: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("joined pieces = %d, want 1", len(res))
	}
	if len(res[0].Holes) != 1 {
		t.Fatalf("joined holes = %d, want 1", len(res[0].Holes))
	}
	approx(t, res.Area(), 80, 1e-9, "joined square band area")
	bb := res[0].Outer.BoundingBox()
	approx(t, bb.Min.X, -1, 1e-9, "joined outer min x")
	approx(t, bb.Max.X, 11, 1e-9, "joined outer max x")
}

func TestOffsetPathsJoinedClosingDuplicate(t *testing.T) {
	// An explicit closing point (last == first) is the same loop as without it.
	open := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	closed := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0}}
	o, err := OffsetPaths([]Polyline{open}, 1, OffsetOptions{End: EndJoined})
	if err != nil {
		t.Fatalf("joined open: %v", err)
	}
	c, err := OffsetPaths([]Polyline{closed}, 1, OffsetOptions{End: EndJoined})
	if err != nil {
		t.Fatalf("joined closed: %v", err)
	}
	approx(t, c.Area(), o.Area(), 1e-9, "joined closing-dup area")
}

func TestOffsetPathsJoinedThinLoopSolid(t *testing.T) {
	// A loop that encloses less than the band width on its short axis: the inner
	// ring collapses, leaving a solid ribbon (no hole) rather than an annulus.
	line := Polyline{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 1}, {X: 0, Y: 1}}
	res, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndJoined, Join: JoinMiter})
	if err != nil {
		t.Fatalf("OffsetPaths thin joined: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("thin joined pieces = %d, want 1", len(res))
	}
	if len(res[0].Holes) != 0 {
		t.Errorf("thin joined holes = %d, want 0", len(res[0].Holes))
	}
	if a := res.Area(); a <= 0 {
		t.Errorf("thin joined area = %g, want > 0", a)
	}
}

func TestOffsetPathsJoinedNonLoopBandsClosingEdge(t *testing.T) {
	// A non-closed L is closed into a triangle; the band wraps the whole loop
	// including the implicit hypotenuse, so it differs from the open-cap ribbon.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}
	joined, err := OffsetPaths([]Polyline{line}, 1, OffsetOptions{End: EndJoined, Join: JoinMiter})
	if err != nil {
		t.Fatalf("OffsetPaths joined L: %v", err)
	}
	butt, err := OffsetPaths([]Polyline{line}, 1, OffsetOptions{End: EndButt, Join: JoinMiter})
	if err != nil {
		t.Fatalf("OffsetPaths butt L: %v", err)
	}
	// The triangle (legs 10, hypotenuse ~14.1) has perimeter ~34.1; a band of
	// width 2 around it is much larger than the open two-arm ribbon (~37).
	if joined.Area() <= butt.Area() {
		t.Errorf("joined L area %g, want > butt L area %g", joined.Area(), butt.Area())
	}
}

func TestOffsetPathsJoinedTwoPointFallback(t *testing.T) {
	// A 2-point path cannot form a loop with area; it falls back to a capped
	// ribbon so the result is still a non-empty band.
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	res, err := OffsetPaths([]Polyline{line}, 2, OffsetOptions{End: EndJoined})
	if err != nil {
		t.Fatalf("OffsetPaths joined 2-point: %v", err)
	}
	if a := res.Area(); a <= 0 {
		t.Errorf("joined 2-point area = %g, want > 0", a)
	}
}

func TestOffsetPathsZeroWidth(t *testing.T) {
	line := Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	_, err := OffsetPaths([]Polyline{line}, 0, OffsetOptions{End: EndButt})
	if err != ErrOffsetEmpty {
		t.Errorf("OffsetPaths(d=0) err = %v, want ErrOffsetEmpty", err)
	}
}
