package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_xor keeps the region covered by exactly one of two overlapping 10×10
// squares. The shared strip drops out, leaving two disjoint 5×10 pieces of total
// area 100.
func Example_xor() {
	a := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	b := geom.New().Point(5, 0).Point(15, 0).Point(15, 10).Point(5, 10).MustBuild()

	out, err := polyclip.Xor(a, b)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 2
	// area:   100
}
