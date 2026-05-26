package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_minkowskiSum sweeps a 2×2 square pattern along an open path. Sweeping
// it down a length-10 segment produces a 12×2 region (the segment dilated by the
// square), area 24.
func Example_minkowskiSum() {
	pattern := geom.Polygon{{X: 0, Y: 0}, {X: 2, Y: 0}, {X: 2, Y: 2}, {X: 0, Y: 2}}
	path := []geom.Point{{X: 0, Y: 0}, {X: 10, Y: 0}}

	out, err := polyclip.MinkowskiSum(pattern, path, false)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 1
	// area:   24
}
