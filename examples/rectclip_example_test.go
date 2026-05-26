package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_rectClip clips a 20×20 square to the rectangle [5,15]×[5,15]. RectClip
// is a sweep-free fast path for the common "clip a layer to the build plate"
// case and returns no error.
func Example_rectClip() {
	m := geom.New().Point(0, 0).Point(20, 0).Point(20, 20).Point(0, 20).MustBuild()
	rect := geom.BBox{Min: geom.Point{X: 5, Y: 5}, Max: geom.Point{X: 15, Y: 15}}

	out := polyclip.RectClip(m, rect)

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 1
	// area:   100
}
