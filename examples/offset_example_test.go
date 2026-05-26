package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_offset inflates and deflates a 10×10 square with miter joins. Positive
// distance grows it (10×10 → 14×14), negative shrinks it (→ 6×6), and a negative
// distance past the inradius collapses the shape to nothing, reported as
// [polyclip.ErrOffsetEmpty].
func Example_offset() {
	square := geom.New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustBuild()
	opts := polyclip.OffsetOptions{Join: polyclip.JoinMiter}

	grown, err := polyclip.Offset(square, 2, opts)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("outward +2: area %g\n", grown.Area())

	shrunk, err := polyclip.Offset(square, -2, opts)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("inward  -2: area %g\n", shrunk.Area())

	_, err = polyclip.Offset(square, -6, opts)
	fmt.Printf("inward  -6: %v\n", err)
	// Output:
	// outward +2: area 196
	// inward  -2: area 36
	// inward  -6: polyclip: offset produced empty result
}
