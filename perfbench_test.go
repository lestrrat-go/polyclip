package polyclip

import (
	"math"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
)

func circle(cx, cy, r float64, n int) geom.Polygon {
	b := geom.New()
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		b.Point(cx+r*math.Cos(a), cy+r*math.Sin(a))
	}
	return b.MustPolygon()
}

func gear(cx, cy, rIn, rOut float64, teeth int) geom.Polygon {
	n := teeth * 2
	b := geom.New()
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		r := rIn
		if i%2 == 0 {
			r = rOut
		}
		b.Point(cx+r*math.Cos(a), cy+r*math.Sin(a))
	}
	return b.MustPolygon()
}

func manyDisjoint(k, vtx int) geom.MultiPolygon {
	b := geom.New()
	for i := range k {
		for j := range k {
			b.NextPiece()
			c := circle(float64(i)*10, float64(j)*10, 3, vtx)
			for _, p := range c {
				b.Point(p.X, p.Y)
			}
		}
	}
	return b.MustBuild()
}

func benchOp(b *testing.B, op func(a, c geom.MultiPolygon) (geom.MultiPolygon, error), a, c geom.MultiPolygon) {
	b.ReportAllocs()
	for range b.N {
		if _, err := op(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnionGears(b *testing.B) {
	a := geom.MultiPolygon{geom.ExPolygon{Outer: gear(0, 0, 30, 50, 200)}}
	c := geom.MultiPolygon{geom.ExPolygon{Outer: gear(15, 15, 30, 50, 200)}}
	benchOp(b, Union, a, c)
}

func BenchmarkIntersectGears(b *testing.B) {
	a := geom.MultiPolygon{geom.ExPolygon{Outer: gear(0, 0, 30, 50, 200)}}
	c := geom.MultiPolygon{geom.ExPolygon{Outer: gear(15, 15, 30, 50, 200)}}
	benchOp(b, Intersect, a, c)
}

func BenchmarkUnionBigCircles(b *testing.B) {
	a := geom.MultiPolygon{geom.ExPolygon{Outer: circle(0, 0, 100, 2000)}}
	c := geom.MultiPolygon{geom.ExPolygon{Outer: circle(50, 0, 100, 2000)}}
	benchOp(b, Union, a, c)
}

func BenchmarkUnionManyPieces(b *testing.B) {
	a := manyDisjoint(20, 16)
	c := geom.MultiPolygon{geom.ExPolygon{Outer: circle(100, 100, 5, 32)}}
	benchOp(b, Union, a, c)
}

// rect is an axis-aligned CCW rectangle on the integer grid.
func rect(x0, y0, w, h int) geom.Polygon {
	return geom.New().
		Point(float64(x0), float64(y0)).
		Point(float64(x0+w), float64(y0)).
		Point(float64(x0+w), float64(y0+h)).
		Point(float64(x0), float64(y0+h)).
		MustPolygon()
}

// brickWall builds a rows*cols staggered grid of bricks. Alternating rows are
// offset by half a brick, so each brick's vertical edges land in the interior
// of the bricks above/below — a dense source of T-junctions, plus collinear
// overlaps along every shared row boundary.
func brickWall(rows, cols, w, h int) geom.MultiPolygon {
	b := geom.New()
	for r := range rows {
		off := 0
		if r%2 == 1 {
			off = w / 2
		}
		for c := range cols {
			b.NextPiece()
			for _, p := range rect(c*w+off, r*h, w, h) {
				b.Point(p.X, p.Y)
			}
		}
	}
	return b.MustBuild()
}

// SplitTJunctions + SplitOverlaps stress: staggered grid. The split loops'
// O(n^3)->O(n^2) improvement (opportunity #2) should show up here and scale
// with size.
func BenchmarkUnionBrickWallSmall(b *testing.B) {
	a := brickWall(14, 14, 6, 3)
	c := geom.MultiPolygon{geom.ExPolygon{Outer: rect(0, 0, 100, 45)}}
	benchOp(b, Union, a, c)
}

func BenchmarkUnionBrickWallLarge(b *testing.B) {
	a := brickWall(24, 24, 6, 3)
	c := geom.MultiPolygon{geom.ExPolygon{Outer: rect(0, 0, 160, 75)}}
	benchOp(b, Union, a, c)
}
