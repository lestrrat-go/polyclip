package polyclip

import (
	"math"
	"testing"
)

func circle(cx, cy, r float64, n int) Polygon {
	p := make(Polygon, n)
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		p[i] = Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}
	return p
}

func gear(cx, cy, rIn, rOut float64, teeth int) Polygon {
	n := teeth * 2
	p := make(Polygon, n)
	for i := range n {
		a := 2 * math.Pi * float64(i) / float64(n)
		r := rIn
		if i%2 == 0 {
			r = rOut
		}
		p[i] = Point{X: cx + r*math.Cos(a), Y: cy + r*math.Sin(a)}
	}
	return p
}

func manyDisjoint(k, vtx int) MultiPolygon {
	m := make(MultiPolygon, 0, k*k)
	for i := range k {
		for j := range k {
			c := circle(float64(i)*10, float64(j)*10, 3, vtx)
			m = append(m, ExPolygon{Outer: c})
		}
	}
	return m
}

func benchOp(b *testing.B, op func(a, c MultiPolygon) (MultiPolygon, error), a, c MultiPolygon) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := op(a, c); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkUnionGears(b *testing.B) {
	a := MultiPolygon{ExPolygon{Outer: gear(0, 0, 30, 50, 200)}}
	c := MultiPolygon{ExPolygon{Outer: gear(15, 15, 30, 50, 200)}}
	benchOp(b, Union, a, c)
}

func BenchmarkIntersectGears(b *testing.B) {
	a := MultiPolygon{ExPolygon{Outer: gear(0, 0, 30, 50, 200)}}
	c := MultiPolygon{ExPolygon{Outer: gear(15, 15, 30, 50, 200)}}
	benchOp(b, Intersect, a, c)
}

func BenchmarkUnionBigCircles(b *testing.B) {
	a := MultiPolygon{ExPolygon{Outer: circle(0, 0, 100, 2000)}}
	c := MultiPolygon{ExPolygon{Outer: circle(50, 0, 100, 2000)}}
	benchOp(b, Union, a, c)
}

func BenchmarkUnionManyPieces(b *testing.B) {
	a := manyDisjoint(20, 16)
	c := MultiPolygon{ExPolygon{Outer: circle(100, 100, 5, 32)}}
	benchOp(b, Union, a, c)
}

// rect is an axis-aligned CCW rectangle on the integer grid.
func rect(x0, y0, w, h int) Polygon {
	return Polygon{
		{X: float64(x0), Y: float64(y0)},
		{X: float64(x0 + w), Y: float64(y0)},
		{X: float64(x0 + w), Y: float64(y0 + h)},
		{X: float64(x0), Y: float64(y0 + h)},
	}
}

// brickWall builds a rows*cols staggered grid of bricks. Alternating rows are
// offset by half a brick, so each brick's vertical edges land in the interior
// of the bricks above/below — a dense source of T-junctions, plus collinear
// overlaps along every shared row boundary.
func brickWall(rows, cols, w, h int) MultiPolygon {
	m := make(MultiPolygon, 0, rows*cols)
	for r := range rows {
		off := 0
		if r%2 == 1 {
			off = w / 2
		}
		for c := range cols {
			m = append(m, ExPolygon{Outer: rect(c*w+off, r*h, w, h)})
		}
	}
	return m
}

// SplitTJunctions + SplitOverlaps stress: staggered grid. The split loops'
// O(n^3)->O(n^2) improvement (opportunity #2) should show up here and scale
// with size.
func BenchmarkUnionBrickWallSmall(b *testing.B) {
	a := brickWall(14, 14, 6, 3)
	c := MultiPolygon{ExPolygon{Outer: rect(0, 0, 100, 45)}}
	benchOp(b, Union, a, c)
}

func BenchmarkUnionBrickWallLarge(b *testing.B) {
	a := brickWall(24, 24, 6, 3)
	c := MultiPolygon{ExPolygon{Outer: rect(0, 0, 160, 75)}}
	benchOp(b, Union, a, c)
}
