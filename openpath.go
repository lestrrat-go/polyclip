package polyclip

import (
	"sort"

	"github.com/lestrrat-go/polyclip/geom"
)

// openCrossEps is the tolerance, in normalized segment parameter, used when
// detecting where an open path crosses a closed boundary and when deduplicating
// near-coincident crossings. Parameters are normalized to [0, 1] along the open
// segment, so the tolerance is independent of the coordinate scale.
const openCrossEps = 1e-9

// clipOpenPaths clips open subject polylines against the closed operands under
// op and returns the surviving open chains. The keep predicate matches
// Clipper2's open-path semantics (IsContributingOpen): a point on an open path
// is retained when —
//
//   - OpIntersect:  it lies inside the clip region;
//   - OpDifference: it lies outside the clip region;
//   - OpXor:        it lies outside the clip region (open paths have no area,
//     so symmetric difference reduces to difference);
//   - OpUnion:      it lies outside both the subject and the clip regions.
//
// Membership is the filled-region test (MultiPolygon.Contains); open clipping
// uses the operands' filled regions regardless of the builder's fill rule.
// Open paths never clip one another, matching Clipper2.
func clipOpenPaths(lines []geom.Polyline, op Operation, s, c geom.MultiPolygon) []geom.Polyline {
	if len(lines) == 0 {
		return nil
	}
	keep, rings := openKeep(op, s, c)
	var result []geom.Polyline
	for _, line := range lines {
		result = clipOpenPath(line, rings, keep, result)
	}
	return result
}

// openKeep returns the per-op keep predicate and the set of closed boundary
// rings the open segments must be split at (every ring whose membership the
// predicate depends on). For OpIntersect/OpDifference/OpXor only the clip region
// matters; OpUnion additionally depends on the subject region.
func openKeep(op Operation, s, c geom.MultiPolygon) (func(geom.Point) bool, [][]geom.Point) {
	switch op {
	case OpIntersect:
		return func(p geom.Point) bool { return c.Contains(p) }, collectRings(c)
	case OpUnion:
		rings := collectRings(s)
		rings = append(rings, collectRings(c)...)
		return func(p geom.Point) bool { return !s.Contains(p) && !c.Contains(p) }, rings
	default: // OpDifference, OpXor
		return func(p geom.Point) bool { return !c.Contains(p) }, collectRings(c)
	}
}

// collectRings returns every boundary ring (each ExPolygon's outer and holes)
// of m as a flat slice of point loops.
func collectRings(m geom.MultiPolygon) [][]geom.Point {
	var rings [][]geom.Point
	for i := range m {
		if len(m[i].Outer) >= 3 {
			rings = append(rings, m[i].Outer)
		}
		for j := range m[i].Holes {
			if len(m[i].Holes[j]) >= 3 {
				rings = append(rings, m[i].Holes[j])
			}
		}
	}
	return rings
}

// clipOpenPath clips one open polyline, splitting each segment at every crossing
// of a boundary ring and keeping the sub-segments whose midpoint satisfies keep.
// Surviving runs that share an endpoint (across path vertices that are not
// boundary crossings) stay in the same output polyline; a dropped sub-segment
// breaks the run. Each kept run with at least two points is appended to acc.
func clipOpenPath(line geom.Polyline, rings [][]geom.Point, keep func(geom.Point) bool, acc []geom.Polyline) []geom.Polyline {
	if len(line) < 2 {
		return acc
	}
	var cur []geom.Point
	flush := func() {
		if len(cur) >= 2 {
			acc = append(acc, geom.Polyline(cur))
		}
		cur = nil
	}
	for i := 0; i+1 < len(line); i++ {
		a, b := line[i], line[i+1]
		if a == b {
			continue
		}
		ts := crossingParams(a, b, rings)
		for k := 0; k+1 < len(ts); k++ {
			s0 := lerpPoint(a, b, ts[k])
			s1 := lerpPoint(a, b, ts[k+1])
			if s0 == s1 {
				continue
			}
			mid := lerpPoint(a, b, (ts[k]+ts[k+1])/2)
			if !keep(mid) {
				flush()
				continue
			}
			if len(cur) > 0 && s0 == cur[len(cur)-1] {
				cur = append(cur, s1)
				continue
			}
			flush()
			cur = []geom.Point{s0, s1}
		}
	}
	flush()
	return acc
}

// crossingParams returns the sorted, deduplicated set of parameters in [0, 1]
// along a→b at which the segment crosses any ring edge, always including the
// endpoints 0 and 1. The midpoint between consecutive parameters lies strictly
// inside a single region, so its membership is unambiguous.
func crossingParams(a, b geom.Point, rings [][]geom.Point) []float64 {
	ts := []float64{0, 1}
	for _, ring := range rings {
		n := len(ring)
		for i := range ring {
			p, q := ring[i], ring[(i+1)%n]
			if t, ok := segParam(a, b, p, q); ok {
				ts = append(ts, t)
			}
		}
	}
	sort.Float64s(ts)
	out := ts[:1]
	for _, t := range ts[1:] {
		if t-out[len(out)-1] > openCrossEps {
			out = append(out, t)
		}
	}
	return out
}

// segParam returns the parameter t along a→b where it meets the segment p→q,
// clamped to [0, 1], reporting false when the segments are parallel or do not
// meet within both extents. Endpoint and vertex touches are included (a small
// tolerance on each parameter) so a path passing exactly through a ring vertex
// still registers a crossing.
func segParam(a, b, p, q geom.Point) (float64, bool) {
	r := b.Sub(a)
	s := q.Sub(p)
	denom := r.Cross(s)
	if denom == 0 {
		return 0, false
	}
	qp := p.Sub(a)
	t := qp.Cross(s) / denom
	u := qp.Cross(r) / denom
	if t < -openCrossEps || t > 1+openCrossEps || u < -openCrossEps || u > 1+openCrossEps {
		return 0, false
	}
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	return t, true
}

// lerpPoint returns the point at parameter t along a→b.
func lerpPoint(a, b geom.Point, t float64) geom.Point {
	return geom.Point{X: a.X + t*(b.X-a.X), Y: a.Y + t*(b.Y-a.Y)}
}
