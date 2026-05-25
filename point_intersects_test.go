package polyclip

import (
	"testing"

	"github.com/stretchr/testify/require"
)

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
