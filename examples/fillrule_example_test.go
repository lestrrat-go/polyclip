package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

// Example_fillRule selects a non-default fill rule via Builder.Fill. The default
// (non-zero) is what the free functions use; even-odd instead fills a region
// crossed by an odd number of edges, so a doubly-covered area reads as a hole.
//
// Two concentric squares are combined under the even-odd rule: the inner square
// sits inside the outer, so its area is covered twice and becomes a hole — the
// result is an annulus (one piece, one hole).
func Example_fillRule() {
	outer := geom.New().Point(0, 0).Point(20, 0).Point(20, 20).Point(0, 20).MustBuild()
	inner := geom.New().Point(5, 5).Point(15, 5).Point(15, 15).Point(5, 15).MustBuild()

	res, err := polyclip.New().
		AddSubject(outer, inner).
		Fill(polyclip.FillEvenOdd).
		Execute(polyclip.OpUnion)
	if err != nil {
		fmt.Println(err)
		return
	}

	fmt.Printf("pieces: %d\n", len(res.Closed))
	fmt.Printf("holes:  %d\n", len(res.Closed[0].Holes))
	fmt.Printf("area:   %g\n", res.Closed.Area())
	// Output:
	// pieces: 1
	// holes:  1
	// area:   300
}
