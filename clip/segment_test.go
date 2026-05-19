package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

func TestNewSegmentCanonicalisation(t *testing.T) {
	a := fixed.Point{X: 0, Y: 10}
	b := fixed.Point{X: 0, Y: 0}

	s := NewSegment(a, b, Subject)
	if s.Bot != b || s.Top != a {
		t.Fatalf("Bot/Top: %+v want Bot=%v Top=%v", s, b, a)
	}
	if !s.Reversed {
		t.Error("Reversed should be true when input a→b runs Top→Bot")
	}
	if s.Start() != a || s.End() != b {
		t.Errorf("Start/End: got %v/%v want %v/%v", s.Start(), s.End(), a, b)
	}
}

func TestNewSegmentSameY(t *testing.T) {
	// Horizontal segment: tie-break by X. Lower X is Bot.
	a := fixed.Point{X: 10, Y: 5}
	b := fixed.Point{X: -5, Y: 5}
	s := NewSegment(a, b, Clip)
	if s.Bot != b || s.Top != a {
		t.Fatalf("Bot/Top tie-break: %+v", s)
	}
	if !s.Horizontal() {
		t.Error("expected horizontal")
	}
}

func TestSegmentDegenerate(t *testing.T) {
	p := fixed.Point{X: 1, Y: 2}
	s := NewSegment(p, p, Subject)
	if !s.Degenerate() {
		t.Error("zero-length segment not degenerate")
	}
}

func TestLessYX(t *testing.T) {
	cases := []struct {
		a, b fixed.Point
		want bool
	}{
		{fixed.Point{X: 0, Y: 0}, fixed.Point{X: 1, Y: 0}, true},  // same Y, larger X on rhs
		{fixed.Point{X: 0, Y: 0}, fixed.Point{X: 0, Y: 1}, true},  // smaller Y on lhs
		{fixed.Point{X: 1, Y: 0}, fixed.Point{X: 0, Y: 0}, false}, // same Y, smaller X on rhs
		{fixed.Point{X: 0, Y: 0}, fixed.Point{X: 0, Y: 0}, false}, // equal
	}
	for _, c := range cases {
		if got := LessYX(c.a, c.b); got != c.want {
			t.Errorf("LessYX(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}
