package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_union merges two 10×10 squares that overlap at a corner and reads the
// resulting geometry back out. They share a 5×5 corner, so the merged area is
// 175 (200 − 25) and the outline is a single L-shaped ring. Iterating the
// returned MultiPolygon and walking each piece's Outer ring is the standard way
// to consume any operation's result.
func Example_union() {
	a := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	b := geom.New().Point(5, 5).Point(15, 5).Point(15, 15).Point(5, 15).MustBuild()

	out, err := polyclip.Union(a, b)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	for i, piece := range out {
		fmt.Printf("piece %d outer:", i)
		for _, v := range piece.Outer {
			fmt.Printf(" (%g,%g)", v.X, v.Y)
		}
		fmt.Println()
	}
	fmt.Printf("area: %g\n", out.Area())
	// Output:
	// pieces: 1
	// piece 0 outer: (15,15) (5,15) (5,10) (0,10) (0,0) (10,0) (10,5) (15,5)
	// area: 175
}
