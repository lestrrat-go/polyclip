package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

func seg(x1, y1, x2, y2 int64) Segment {
	return NewSegment(
		fixed.Point{X: fixed.Coord(x1), Y: fixed.Coord(y1)},
		fixed.Point{X: fixed.Coord(x2), Y: fixed.Coord(y2)},
		Subject,
	)
}

func TestIntersectProperCross(t *testing.T) {
	a := seg(0, 0, 10, 10)
	b := seg(0, 10, 10, 0)
	r := Intersect(a, b)
	if r.Kind != ProperCross {
		t.Fatalf("Kind: %v want ProperCross", r.Kind)
	}
	want := fixed.Point{X: 5, Y: 5}
	if r.P != want {
		t.Errorf("P: %+v want %+v", r.P, want)
	}
}

func TestIntersectTouchAtEndpoint(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 5, 10)
	r := Intersect(a, b)
	if r.Kind != Touch {
		t.Fatalf("Kind: %v want Touch", r.Kind)
	}
	want := fixed.Point{X: 5, Y: 0}
	if r.P != want {
		t.Errorf("P: %+v want %+v", r.P, want)
	}
}

func TestIntersectSharedEndpoint(t *testing.T) {
	a := seg(0, 0, 5, 5)
	b := seg(5, 5, 10, 0)
	r := Intersect(a, b)
	if r.Kind != Touch {
		t.Fatalf("Kind: %v want Touch", r.Kind)
	}
	want := fixed.Point{X: 5, Y: 5}
	if r.P != want {
		t.Errorf("P: %+v want %+v", r.P, want)
	}
}

func TestIntersectCollinearOverlap(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 15, 0)
	r := Intersect(a, b)
	if r.Kind != CollinearOverlap {
		t.Fatalf("Kind: %v want CollinearOverlap", r.Kind)
	}
	wantP := fixed.Point{X: 5, Y: 0}
	wantQ := fixed.Point{X: 10, Y: 0}
	if r.P != wantP || r.Q != wantQ {
		t.Errorf("overlap: P=%v Q=%v want %v %v", r.P, r.Q, wantP, wantQ)
	}
}

func TestIntersectCollinearTouch(t *testing.T) {
	// Two collinear segments meeting at exactly one point.
	a := seg(0, 0, 5, 0)
	b := seg(5, 0, 10, 0)
	r := Intersect(a, b)
	if r.Kind != Touch {
		t.Fatalf("Kind: %v want Touch", r.Kind)
	}
	if r.P != (fixed.Point{X: 5, Y: 0}) {
		t.Errorf("P: %v", r.P)
	}
}

func TestIntersectCollinearDisjoint(t *testing.T) {
	a := seg(0, 0, 5, 0)
	b := seg(10, 0, 15, 0)
	if r := Intersect(a, b); r.Kind != NoCrossing {
		t.Errorf("Kind: %v want NoCrossing", r.Kind)
	}
}

func TestIntersectParallel(t *testing.T) {
	a := seg(0, 0, 10, 10)
	b := seg(0, 5, 10, 15) // parallel, offset
	if r := Intersect(a, b); r.Kind != NoCrossing {
		t.Errorf("Kind: %v want NoCrossing", r.Kind)
	}
}

func TestIntersectNonParallelNoOverlap(t *testing.T) {
	a := seg(0, 0, 1, 0)
	b := seg(10, 10, 11, 11) // far away, not parallel
	if r := Intersect(a, b); r.Kind != NoCrossing {
		t.Errorf("Kind: %v want NoCrossing", r.Kind)
	}
}

func TestIntersectTJunction(t *testing.T) {
	// b's bottom endpoint lies in the interior of a.
	a := seg(0, 0, 10, 0)
	b := seg(5, 0, 5, 10)
	r := Intersect(a, b)
	if r.Kind != Touch {
		t.Fatalf("Kind: %v want Touch (T-junction)", r.Kind)
	}
	if r.P != (fixed.Point{X: 5, Y: 0}) {
		t.Errorf("P: %v", r.P)
	}
}

func TestIntersectCollinearVertical(t *testing.T) {
	a := seg(3, 0, 3, 10)
	b := seg(3, 5, 3, 15)
	r := Intersect(a, b)
	if r.Kind != CollinearOverlap {
		t.Fatalf("Kind: %v want CollinearOverlap", r.Kind)
	}
	if r.P != (fixed.Point{X: 3, Y: 5}) || r.Q != (fixed.Point{X: 3, Y: 10}) {
		t.Errorf("overlap: %v %v", r.P, r.Q)
	}
}

func TestIntersectCollinearContained(t *testing.T) {
	a := seg(0, 0, 10, 0)
	b := seg(3, 0, 7, 0)
	r := Intersect(a, b)
	if r.Kind != CollinearOverlap {
		t.Fatalf("Kind: %v want CollinearOverlap", r.Kind)
	}
	if r.P != (fixed.Point{X: 3, Y: 0}) || r.Q != (fixed.Point{X: 7, Y: 0}) {
		t.Errorf("overlap: %v %v", r.P, r.Q)
	}
}

func TestIntersectAtOrigin(t *testing.T) {
	// Both segments start at the origin.
	a := seg(0, 0, 10, 0)
	b := seg(0, 0, 0, 10)
	r := Intersect(a, b)
	if r.Kind != Touch {
		t.Fatalf("Kind: %v want Touch", r.Kind)
	}
	if r.P != (fixed.Point{X: 0, Y: 0}) {
		t.Errorf("P: %v", r.P)
	}
}
