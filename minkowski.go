package polyclip

import "github.com/lestrrat-go/polyclip/geom"

// MinkowskiSum returns the Minkowski sum of pattern and path: the region swept
// by placing a copy of pattern at every point of path. It is built by emitting,
// for each consecutive pair of path vertices, the quadrilateral strip between
// the two pattern placements, then unioning all the strips (and placements)
// under the non-zero fill rule via [UnionAll].
//
// pattern is the shape being swept (a closed ring); path is the trajectory.
// When closed is true the path is treated as a closed loop (a strip is also
// emitted between the last and first vertices); when false it is an open
// polyline. An empty pattern or path returns an empty MultiPolygon.
//
// This is a faithful port of Clipper2's MinkowskiSum and produces the same
// output for the same inputs.
func MinkowskiSum(pattern geom.Polygon, path []geom.Point, closed bool) (geom.MultiPolygon, error) {
	return minkowski(pattern, path, true, closed)
}

// MinkowskiDiff returns the Minkowski difference of pattern and path: the same
// construction as [MinkowskiSum] but with pattern reflected through the origin
// (each placement is path vertex minus pattern vertex). It is a faithful port
// of Clipper2's MinkowskiDiff. See [MinkowskiSum] for the closed/open and empty
// semantics.
func MinkowskiDiff(pattern geom.Polygon, path []geom.Point, closed bool) (geom.MultiPolygon, error) {
	return minkowski(pattern, path, false, closed)
}

// minkowski builds the quad strip swept by pattern along path and unions it.
// When isSum is true each placement is path[i]+pattern[k]; otherwise it is the
// reflected path[i]-pattern[k]. Each quad is normalized to positive (CCW)
// winding before the union so the non-zero rule fills it consistently.
func minkowski(pattern geom.Polygon, path []geom.Point, isSum, closed bool) (geom.MultiPolygon, error) {
	patLen, pathLen := len(pattern), len(path)
	if patLen == 0 || pathLen == 0 {
		return geom.MultiPolygon{}, nil
	}

	// tmp[i] is pattern placed (or reflected) at path[i].
	tmp := make([][]geom.Point, pathLen)
	for i, p := range path {
		placed := make([]geom.Point, patLen)
		for k, pt := range pattern {
			if isSum {
				placed[k] = geom.Point{X: p.X + pt.X, Y: p.Y + pt.Y}
			} else {
				placed[k] = geom.Point{X: p.X - pt.X, Y: p.Y - pt.Y}
			}
		}
		tmp[i] = placed
	}

	// A closed path also strips between the last and first vertices, so it
	// starts paired with the final placement and visits every vertex; an open
	// path starts at vertex 0 and skips the first iteration (delta).
	delta := 1
	g := 0
	if closed {
		delta = 0
		g = pathLen - 1
	}
	quads := make([]geom.MultiPolygon, 0, (pathLen-delta)*patLen)
	h := patLen - 1
	for i := delta; i < pathLen; i++ {
		for j := range patLen {
			quad := geom.Polygon{tmp[g][h], tmp[i][h], tmp[i][j], tmp[g][j]}
			if !quad.IsCCW() {
				quad.Reverse()
			}
			quads = append(quads, geom.MultiPolygon{{Outer: quad}})
			h = j
		}
		g = i
	}
	return UnionAll(quads...)
}
