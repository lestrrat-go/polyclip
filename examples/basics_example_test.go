package examples_test

import (
	"fmt"

	"github.com/lestrrat-go/polyclip/geom"
)

// Example_basics is the data model. A MultiPolygon is a slice of ExPolygon
// pieces; each ExPolygon is an Outer ring plus zero or more Holes; a ring
// (Polygon) is a slice of Points whose closing edge — last point back to first —
// is implicit, so you never repeat the first point. By convention outer rings
// wind counter-clockwise and holes clockwise, but operations accept either and
// normalize.
//
// This example builds one 10×10 square with a 2×2 hole two ways — an explicit
// literal (so the structure is visible) and the fluent geom.Builder — then reads
// the pieces back out.
func Example_basics() {
	// Explicit literal: the type structure spelled out.
	m := geom.MultiPolygon{
		{
			Outer: geom.Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
			Holes: []geom.Polygon{
				{{X: 4, Y: 4}, {X: 4, Y: 6}, {X: 6, Y: 6}, {X: 6, Y: 4}},
			},
		},
	}

	// The same shape via the fluent builder (winding normalized on Build). Build
	// a hole ring with a second builder's Polygon().
	hole := geom.New().Point(4, 4).Point(4, 6).Point(6, 6).Point(6, 4).MustPolygon()
	built := geom.New().
		Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
		Hole(hole).
		MustBuild()

	// Read the parts.
	fmt.Printf("pieces:     %d\n", len(m))
	fmt.Printf("outer area: %g\n", m[0].Outer.Area())
	fmt.Printf("holes:      %d\n", len(m[0].Holes))
	fmt.Printf("net area:   %g\n", m.Area()) // outer minus holes
	fmt.Printf("(5,1) inside?  %t\n", m.Contains(geom.Point{X: 5, Y: 1}))
	fmt.Printf("(5,5) inside?  %t\n", m.Contains(geom.Point{X: 5, Y: 5})) // in the hole
	fmt.Printf("built net area: %g\n", built.Area())
	// Output:
	// pieces:     1
	// outer area: 100
	// holes:      1
	// net area:   96
	// (5,1) inside?  true
	// (5,5) inside?  false
	// built net area: 96
}
