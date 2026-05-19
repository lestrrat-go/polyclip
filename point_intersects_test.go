package polyclip

import "testing"

func TestBBoxIntersects(t *testing.T) {
	cases := []struct {
		name string
		a, b BBox
		want bool
	}{
		{
			name: "identical",
			a:    BBox{Min: Point{0, 0}, Max: Point{10, 10}},
			b:    BBox{Min: Point{0, 0}, Max: Point{10, 10}},
			want: true,
		},
		{
			name: "overlapping",
			a:    BBox{Min: Point{0, 0}, Max: Point{10, 10}},
			b:    BBox{Min: Point{5, 5}, Max: Point{15, 15}},
			want: true,
		},
		{
			name: "touching at a corner",
			a:    BBox{Min: Point{0, 0}, Max: Point{5, 5}},
			b:    BBox{Min: Point{5, 5}, Max: Point{10, 10}},
			want: true,
		},
		{
			name: "touching along an edge",
			a:    BBox{Min: Point{0, 0}, Max: Point{5, 5}},
			b:    BBox{Min: Point{5, 0}, Max: Point{10, 5}},
			want: true,
		},
		{
			name: "strictly disjoint on X",
			a:    BBox{Min: Point{0, 0}, Max: Point{5, 5}},
			b:    BBox{Min: Point{6, 0}, Max: Point{10, 5}},
			want: false,
		},
		{
			name: "strictly disjoint on Y",
			a:    BBox{Min: Point{0, 0}, Max: Point{5, 5}},
			b:    BBox{Min: Point{0, 6}, Max: Point{5, 10}},
			want: false,
		},
		{
			name: "empty a",
			a:    EmptyBBox(),
			b:    BBox{Min: Point{0, 0}, Max: Point{5, 5}},
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
		if got := c.a.Intersects(c.b); got != c.want {
			t.Errorf("%s: %v want %v", c.name, got, c.want)
		}
		// Symmetric.
		if got := c.b.Intersects(c.a); got != c.want {
			t.Errorf("%s (symmetric): %v want %v", c.name, got, c.want)
		}
	}
}
