package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_difference subtracts a centered 10×10 square from a 20×20 square. The
// subtracted region is fully interior, so the result is one piece whose Outer is
// the 20×20 boundary and whose single Hole is the cut-out — read both off the
// returned ExPolygon. Holes come back wound clockwise (opposite the outer).
func Example_difference() {
	outer := geom.New().Point(0, 0).Point(20, 0).Point(20, 20).Point(0, 20).MustBuild()
	inner := geom.New().Point(5, 5).Point(15, 5).Point(15, 15).Point(5, 15).MustBuild()

	out, err := polyclip.Difference(outer, inner)
	if err != nil {
		fmt.Println(err)
		return
	}

	piece := out[0]
	fmt.Printf("outer:")
	for _, v := range piece.Outer {
		fmt.Printf(" (%g,%g)", v.X, v.Y)
	}
	fmt.Printf("\nhole:")
	for _, v := range piece.Holes[0] {
		fmt.Printf(" (%g,%g)", v.X, v.Y)
	}
	fmt.Printf("\nnet area: %g\n", out.Area())
	// Output:
	// outer: (20,20) (0,20) (0,0) (20,0)
	// hole: (15,15) (15,5) (5,5) (5,15)
	// net area: 300
}
