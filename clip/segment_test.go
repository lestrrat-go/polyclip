package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
	"github.com/stretchr/testify/require"
)

func TestNewSegmentCanonicalisation(t *testing.T) {
	a := fixed.Point{X: 0, Y: 10}
	b := fixed.Point{X: 0, Y: 0}

	s := NewSegment(a, b, Subject)
	require.True(t, s.Bot == b && s.Top == a, "Bot/Top: %+v want Bot=%v Top=%v", s, b, a)
	require.True(t, s.Reversed, "Reversed should be true when input a→b runs Top→Bot")
	require.True(t, s.Start() == a && s.End() == b, "Start/End: got %v/%v want %v/%v", s.Start(), s.End(), a, b)
}

func TestNewSegmentSameY(t *testing.T) {
	// Horizontal segment: tie-break by X. Lower X is Bot.
	a := fixed.Point{X: 10, Y: 5}
	b := fixed.Point{X: -5, Y: 5}
	s := NewSegment(a, b, Clip)
	require.True(t, s.Bot == b && s.Top == a, "Bot/Top tie-break: %+v", s)
	require.True(t, s.Horizontal(), "expected horizontal")
}

func TestSegmentDegenerate(t *testing.T) {
	p := fixed.Point{X: 1, Y: 2}
	s := NewSegment(p, p, Subject)
	require.True(t, s.Degenerate(), "zero-length segment not degenerate")
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
		require.Equal(t, c.want, LessYX(c.a, c.b), "LessYX(%v,%v)=%v want %v", c.a, c.b, LessYX(c.a, c.b), c.want)
	}
}
