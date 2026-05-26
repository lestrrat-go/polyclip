package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_builder uses the accumulator Builder, the general entry point behind
// the free functions. Beyond closed subjects and clips it can also clip OPEN
// polylines in the same pass — something Union/Intersect/… cannot. Execute
// returns a Result whose Closed field holds the polygon output and whose Open
// field holds the surviving open chains.
//
// Here a line crossing a 10×10 square is intersected with it: the closed output
// is empty (an open path has no area) and the surviving segment is the portion
// inside the square.
func Example_builder() {
	square := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	line := geom.Polyline{{X: -5, Y: 5}, {X: 15, Y: 5}} // enters at x=0, leaves at x=10

	res, err := polyclip.New().
		AddClip(square).
		AddOpenSubject(line).
		Execute(polyclip.OpIntersect)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("closed pieces: %d\n", len(res.Closed))
	fmt.Printf("open chains:   %d\n", len(res.Open))
	fmt.Printf("clipped line:")
	for _, v := range res.Open[0] {
		fmt.Printf(" (%g,%g)", v.X, v.Y)
	}
	fmt.Println()
	// Output:
	// closed pieces: 0
	// open chains:   1
	// clipped line: (0,5) (10,5)
}
