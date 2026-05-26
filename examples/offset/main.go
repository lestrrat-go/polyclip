// Command offset demonstrates polyclip.Offset for outward, inward,
// and round-joined cases. Run with:
//
//	go run ./examples/offset
package main

import (
	"fmt"
	"math"

	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

func pt(x, y float64) geom.Point { return geom.Point{X: x, Y: y} }

func main() {
	square := geom.MultiPolygon{{
		Outer: geom.Polygon{pt(0, 0), pt(10, 0), pt(10, 10), pt(0, 10)},
	}}
	fmt.Printf("Input: 10x10 CCW square (area %v)\n\n", square.Area())

	// Outward miter: 10x10 grows to 14x14 (area 196).
	out, err := polyclip.Offset(square, 2, polyclip.OffsetOptions{Join: polyclip.JoinMiter})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Outward by +2 (miter): area = %v (expected 196)\n", out.Area())

	// Outward round: 14x14 with quarter-circle corners.
	out, err = polyclip.Offset(square, 2, polyclip.OffsetOptions{Join: polyclip.JoinRound, ArcTol: 0.05})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Outward by +2 (round): area = %v (expected ~%v)\n",
		out.Area(), 14.0*14.0-16+math.Pi*4)

	// Inward miter: 10x10 shrinks to 6x6.
	out, err = polyclip.Offset(square, -2, polyclip.OffsetOptions{Join: polyclip.JoinMiter})
	if err != nil {
		panic(err)
	}
	fmt.Printf("Inward by -2 (miter): area = %v (expected 36)\n", out.Area())

	// Inward overshoot: |d|=6 > inradius (5) → collapses to empty.
	_, err = polyclip.Offset(square, -6, polyclip.OffsetOptions{Join: polyclip.JoinMiter})
	fmt.Printf("Inward by -6 (overshoot): err = %v\n", err)
}
