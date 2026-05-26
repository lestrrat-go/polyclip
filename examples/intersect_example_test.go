package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_intersect keeps only the region covered by both 10×10 squares — the
// shared 5×10 strip, area 50.
func Example_intersect() {
	a := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	b := geom.New().Point(5, 0).Point(15, 0).Point(15, 10).Point(5, 10).MustBuild()

	out, err := polyclip.Intersect(a, b)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 1
	// area:   50
}
