// Command union demonstrates polyclip.Union on two overlapping diamonds
// and a hole-preserving disjoint case. Run with:
//
//	go run ./examples/union
package main

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
)

func pt(x, y float64) polyclip.Point { return polyclip.Point{X: x, Y: y} }

func main() {
	// Two overlapping CCW diamonds. Diamonds have no horizontal edges,
	// so the engine handles even densely-overlapping cases robustly.
	// Each diamond has half-diagonal 10 (area 200); they overlap by 50%.
	a := polyclip.MultiPolygon{{
		Outer: polyclip.Polygon{pt(0, -10), pt(10, 0), pt(0, 10), pt(-10, 0)},
	}}
	b := polyclip.MultiPolygon{{
		Outer: polyclip.Polygon{pt(10, -10), pt(20, 0), pt(10, 10), pt(0, 0)},
	}}

	out, err := polyclip.Union(a, b)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Union of two overlapping diamonds:\n")
	fmt.Printf("  input areas:   %v + %v = %v\n", a.Area(), b.Area(), a.Area()+b.Area())
	fmt.Printf("  output area:   %v (overlap removed)\n", out.Area())
	fmt.Printf("  output pieces: %d\n\n", len(out))

	// Disjoint inputs with a hole — Union preserves topology without engine work.
	holed := polyclip.MultiPolygon{{
		Outer: polyclip.Polygon{pt(0, 0), pt(20, 0), pt(20, 20), pt(0, 20)},
		Holes: []polyclip.Polygon{
			{pt(8, 8), pt(8, 12), pt(12, 12), pt(12, 8)}, // CW hole
		},
	}}
	far := polyclip.MultiPolygon{{
		Outer: polyclip.Polygon{pt(100, 100), pt(110, 100), pt(110, 110), pt(100, 110)},
	}}
	out, err = polyclip.Union(holed, far)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Union of disjoint regions (one with a hole):\n")
	fmt.Printf("  output pieces: %d\n", len(out))
	fmt.Printf("  piece[0] outer area: %v, holes: %d\n", out[0].Outer.Area(), len(out[0].Holes))
	fmt.Printf("  piece[1] outer area: %v\n", out[1].Outer.Area())
}
