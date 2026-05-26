package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

func TestBBoxIntersects(t *testing.T) {
	cases := []struct {
		name string
		a, b geom.BBox
		want bool
	}{
		{
			name: "identical",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			b:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			want: true,
		},
		{
			name: "overlapping",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			b:    geom.BBox{Min: geom.Point{X: 5, Y: 5}, Max: geom.Point{X: 15, Y: 15}},
			want: true,
		},
		{
			name: "touching at a corner",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			b:    geom.BBox{Min: geom.Point{X: 5, Y: 5}, Max: geom.Point{X: 10, Y: 10}},
			want: true,
		},
		{
			name: "touching along an edge",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			b:    geom.BBox{Min: geom.Point{X: 5, Y: 0}, Max: geom.Point{X: 10, Y: 5}},
			want: true,
		},
		{
			name: "strictly disjoint on X",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			b:    geom.BBox{Min: geom.Point{X: 6, Y: 0}, Max: geom.Point{X: 10, Y: 5}},
			want: false,
		},
		{
			name: "strictly disjoint on Y",
			a:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			b:    geom.BBox{Min: geom.Point{X: 0, Y: 6}, Max: geom.Point{X: 5, Y: 10}},
			want: false,
		},
		{
			name: "empty a",
			a:    geom.EmptyBBox(),
			b:    geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			want: false,
		},
		{
			name: "both empty",
			a:    geom.EmptyBBox(),
			b:    geom.EmptyBBox(),
			want: false,
		},
	}
	for _, c := range cases {
		require.Equal(t, c.want, c.a.Intersects(c.b), "%s: %v want %v", c.name, c.a.Intersects(c.b), c.want)
		// Symmetric.
		require.Equal(t, c.want, c.b.Intersects(c.a), "%s (symmetric): %v want %v", c.name, c.b.Intersects(c.a), c.want)
	}
}
