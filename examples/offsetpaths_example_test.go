package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_offsetPaths offsets an open polyline into a closed ribbon. A length-10
// horizontal segment offset by 1 to each side with butt end caps yields a 10×2
// rectangle (area 20).
func Example_offsetPaths() {
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}

	out, err := polyclip.OffsetPaths([]geom.Polyline{line}, 1, polyclip.OffsetOptions{End: polyclip.EndButt})
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(out))
	fmt.Printf("area:   %g\n", out.Area())
	// Output:
	// pieces: 1
	// area:   20
}
