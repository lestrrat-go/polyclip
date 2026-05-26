package geom

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBBoxEmpty(t *testing.T) {
	e := EmptyBBox()
	require.True(t, e.Empty(), "EmptyBBox() should be empty, got Min=%v Max=%v", e.Min, e.Max)
	// Zero BBox represents the single point at the origin, not empty.
	var z BBox
	require.False(t, z.Empty(), "zero BBox should not be Empty()")
}

func TestBBoxAdd(t *testing.T) {
	b := EmptyBBox()
	b = b.Add(Point{X: 1, Y: 2})
	b = b.Add(Point{X: -1, Y: 5})
	b = b.Add(Point{X: 4, Y: 3})
	want := BBox{Min: Point{X: -1, Y: 2}, Max: Point{X: 4, Y: 5}}
	require.Equal(t, want, b, "Add: got %+v want %+v", b, want)
}

func TestBBoxUnion(t *testing.T) {
	a := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	b := BBox{Min: Point{X: 5, Y: -5}, Max: Point{X: 20, Y: 5}}
	got := a.Union(b)
	want := BBox{Min: Point{X: 0, Y: -5}, Max: Point{X: 20, Y: 10}}
	require.Equal(t, want, got, "Union: got %+v want %+v", got, want)
	require.Equal(t, a, a.Union(EmptyBBox()), "Union with empty changed value: %+v", a.Union(EmptyBBox()))
	require.Equal(t, a, EmptyBBox().Union(a), "empty.Union(a) != a: %+v", EmptyBBox().Union(a))
}

func TestBBoxContains(t *testing.T) {
	b := BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}}
	cases := []struct {
		p  Point
		in bool
	}{
		{Point{X: 5, Y: 5}, true},
		{Point{X: 0, Y: 0}, true},
		{Point{X: 10, Y: 10}, true},
		{Point{X: 10, Y: 5}, true},
		{Point{X: -0.001, Y: 5}, false},
		{Point{X: 5, Y: 10.001}, false},
	}
	for _, c := range cases {
		require.Equal(t, c.in, b.Contains(c.p), "Contains(%v): got %v want %v", c.p, b.Contains(c.p), c.in)
	}
	require.False(t, EmptyBBox().Contains(Point{X: 0, Y: 0}), "empty bbox should contain no points")
}

func TestBBoxWidthHeight(t *testing.T) {
	b := BBox{Min: Point{X: 1, Y: 2}, Max: Point{X: 4, Y: 7}}
	require.Equal(t, 3.0, b.Width(), "Width: %v want 3", b.Width())
	require.Equal(t, 5.0, b.Height(), "Height: %v want 5", b.Height())
	e := EmptyBBox()
	require.True(t, e.Width() == 0 && e.Height() == 0, "empty box w/h should be 0/0, got %v/%v", e.Width(), e.Height())
}

// Sanity: math.Inf used in EmptyBBox should behave as expected with Empty().
func TestBBoxEmptyInfinity(t *testing.T) {
	e := EmptyBBox()
	require.True(t, math.IsInf(e.Min.X, +1) && math.IsInf(e.Max.X, -1), "EmptyBBox infinities wrong: %+v", e)
}

func TestBBoxIntersects(t *testing.T) {
	cases := []struct {
		name string
		a, b BBox
		want bool
	}{
		{
			name: "identical",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}},
			b:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}},
			want: true,
		},
		{
			name: "overlapping",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 10, Y: 10}},
			b:    BBox{Min: Point{X: 5, Y: 5}, Max: Point{X: 15, Y: 15}},
			want: true,
		},
		{
			name: "touching at a corner",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}},
			b:    BBox{Min: Point{X: 5, Y: 5}, Max: Point{X: 10, Y: 10}},
			want: true,
		},
		{
			name: "touching along an edge",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}},
			b:    BBox{Min: Point{X: 5, Y: 0}, Max: Point{X: 10, Y: 5}},
			want: true,
		},
		{
			name: "strictly disjoint on X",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}},
			b:    BBox{Min: Point{X: 6, Y: 0}, Max: Point{X: 10, Y: 5}},
			want: false,
		},
		{
			name: "strictly disjoint on Y",
			a:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}},
			b:    BBox{Min: Point{X: 0, Y: 6}, Max: Point{X: 5, Y: 10}},
			want: false,
		},
		{
			name: "empty a",
			a:    EmptyBBox(),
			b:    BBox{Min: Point{X: 0, Y: 0}, Max: Point{X: 5, Y: 5}},
			want: false,
		},
		{
			name: "both empty",
			a:    EmptyBBox(),
			b:    EmptyBBox(),
			want: false,
		},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.a.Intersects(c.b), "%s: %v want %v", c.name, c.a.Intersects(c.b), c.want)
		// Symmetric.
		require.Equal(t, c.want, c.b.Intersects(c.a), "%s (symmetric): %v want %v", c.name, c.b.Intersects(c.a), c.want)
	}
}
