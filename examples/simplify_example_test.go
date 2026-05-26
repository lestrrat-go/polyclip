package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_simplify resolves a self-intersecting "bowtie" ring into simple,
// non-self-intersecting pieces. The figure-eight splits at its crossing into two
// triangles (total area 50). This is the correct way to clean self-intersecting
// input — Union of a shape with itself does not, because identical inputs hit an
// idempotency short-circuit.
func Example_simplify() {
	bowtie := geom.New().Point(0, 0).Point(10, 10).Point(10, 0).Point(0, 10).MustBuild()

	out, err := polyclip.Simplify(bowtie)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 2
	// area:   50
}
