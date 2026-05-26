package polyclip

import "github.com/lestrrat-go/polyclip/geom"

// RectClip clips every ring of m against the axis-aligned rectangle rect and
// returns the clipped region. It is a specialized, sweep-free fast path for the
// common "clip a layer to the build plate" case: each ring is clipped in O(n)
// by the Sutherland–Hodgman algorithm against the four rectangle edges, rather
// than running the general boolean sweep.
//
// The result covers exactly the same region as Intersect(m, rectAsPolygon) — it
// is validated against Intersect for area parity. One representational
// difference: where the rectangle splits a single concave ring into disjoint
// pieces, Sutherland–Hodgman returns them as one ring whose boundary runs along
// the rectangle edge between the pieces (a zero-width seam), rather than as
// separate ExPolygon values. The enclosed area is identical; pass the result
// through Simplify if cleanly separated pieces are required.
//
// Each input ExPolygon is clipped independently (outer ring and every hole),
// preserving its hole structure: a hole stays nested in its outer because both
// are clipped by the same rectangle. Rings that clip away to nothing are
// dropped, as is any ExPolygon whose outer ring vanishes. An empty rect (see
// [BBox.Empty]) yields an empty result. Output winding follows the library
// convention: outer rings CCW, holes CW.
func RectClip(m geom.MultiPolygon, rect geom.BBox) geom.MultiPolygon {
	if rect.Empty() || len(m) == 0 {
		return geom.MultiPolygon{}
	}
	out := make(geom.MultiPolygon, 0, len(m))
	for i := range m {
		outer := clipRingToRect(m[i].Outer, rect)
		if len(outer) < 3 {
			continue
		}
		if !outer.IsCCW() {
			outer.Reverse()
		}
		ex := geom.ExPolygon{Outer: outer}
		for _, h := range m[i].Holes {
			hole := clipRingToRect(h, rect)
			if len(hole) < 3 {
				continue
			}
			if hole.IsCCW() {
				hole.Reverse()
			}
			ex.Holes = append(ex.Holes, hole)
		}
		out = append(out, ex)
	}
	return out
}

// RectClipLines clips each open polyline in lines against the axis-aligned
// rectangle rect, returning the sub-paths that lie inside it. A polyline that
// crosses the boundary repeatedly is split into one output polyline per inside
// run, in input order; the contiguous portions are stitched back together so a
// path that merely touches the boundary at a vertex stays a single polyline.
//
// Unlike [RectClip], open paths carry no interior, so each segment is clipped
// independently (Liang–Barsky) and no boundary seam is introduced. Polylines
// with fewer than two points, and any clipped fragment that degenerates to a
// single point, are dropped. An empty rect yields no output.
func RectClipLines(lines []geom.Polyline, rect geom.BBox) []geom.Polyline {
	if rect.Empty() || len(lines) == 0 {
		return nil
	}
	var result []geom.Polyline
	for _, line := range lines {
		result = clipPolylineToRect(line, rect, result)
	}
	return result
}

// clipPolylineToRect clips one polyline and appends each resulting inside run to
// acc. Consecutive segments whose clipped pieces share an endpoint are kept in
// the same output polyline; a gap (a segment with no inside portion, or a
// re-entry at a different point) starts a new one.
func clipPolylineToRect(line geom.Polyline, rect geom.BBox, acc []geom.Polyline) []geom.Polyline {
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
		p0, p1, ok := clipSegmentToRect(line[i], line[i+1], rect)
		if !ok {
			flush()
			continue
		}
		if len(cur) > 0 && p0 == cur[len(cur)-1] {
			if p1 != cur[len(cur)-1] {
				cur = append(cur, p1)
			}
			continue
		}
		flush()
		if p0 != p1 {
			cur = []geom.Point{p0, p1}
		}
	}
	flush()
	return acc
}

// clipRingToRect runs Sutherland–Hodgman, clipping the ring against the four
// half-planes of rect in turn. The result preserves the ring's winding
// direction and is deduplicated of consecutive coincident vertices; a ring that
// clips away returns fewer than three points.
func clipRingToRect(poly geom.Polygon, rect geom.BBox) geom.Polygon {
	if len(poly) < 3 {
		return nil
	}
	pts := []geom.Point(poly)
	// Left: x >= Min.X.
	pts = clipHalfplane(pts, func(p geom.Point) bool { return p.X >= rect.Min.X },
		func(a, b geom.Point) geom.Point { return intersectVert(a, b, rect.Min.X) })
	// Right: x <= Max.X.
	pts = clipHalfplane(pts, func(p geom.Point) bool { return p.X <= rect.Max.X },
		func(a, b geom.Point) geom.Point { return intersectVert(a, b, rect.Max.X) })
	// Bottom: y >= Min.Y.
	pts = clipHalfplane(pts, func(p geom.Point) bool { return p.Y >= rect.Min.Y },
		func(a, b geom.Point) geom.Point { return intersectHoriz(a, b, rect.Min.Y) })
	// Top: y <= Max.Y.
	pts = clipHalfplane(pts, func(p geom.Point) bool { return p.Y <= rect.Max.Y },
		func(a, b geom.Point) geom.Point { return intersectHoriz(a, b, rect.Max.Y) })
	return geom.Polygon(dedupRingVertices(pts))
}

// clipHalfplane returns the portion of the closed ring in that lies on the
// inside of a single half-plane, inserting intersection points where edges
// cross the boundary. inside reports membership; isect returns where the edge
// a→b meets the boundary line.
func clipHalfplane(in []geom.Point, inside func(geom.Point) bool, isect func(a, b geom.Point) geom.Point) []geom.Point {
	if len(in) == 0 {
		return nil
	}
	out := make([]geom.Point, 0, len(in)+4)
	prev := in[len(in)-1]
	prevIn := inside(prev)
	for _, cur := range in {
		curIn := inside(cur)
		switch {
		case curIn && prevIn:
			out = append(out, cur)
		case curIn && !prevIn:
			out = append(out, isect(prev, cur), cur)
		case !curIn && prevIn:
			out = append(out, isect(prev, cur))
		}
		prev, prevIn = cur, curIn
	}
	return out
}

// intersectVert returns where the segment a→b crosses the vertical line x=cx.
func intersectVert(a, b geom.Point, cx float64) geom.Point {
	t := (cx - a.X) / (b.X - a.X)
	return geom.Point{X: cx, Y: a.Y + t*(b.Y-a.Y)}
}

// intersectHoriz returns where the segment a→b crosses the horizontal line y=cy.
func intersectHoriz(a, b geom.Point, cy float64) geom.Point {
	t := (cy - a.Y) / (b.Y - a.Y)
	return geom.Point{X: a.X + t*(b.X-a.X), Y: cy}
}

// dedupRingVertices removes runs of identical points, treating the slice as a
// closed ring (the last point is also compared against the first).
func dedupRingVertices(pts []geom.Point) []geom.Point {
	if len(pts) < 2 {
		return pts
	}
	out := pts[:0:0]
	for i, p := range pts {
		if i > 0 && p == pts[i-1] {
			continue
		}
		out = append(out, p)
	}
	for len(out) >= 2 && out[len(out)-1] == out[0] {
		out = out[:len(out)-1]
	}
	return out
}

// clipSegmentToRect clips the open segment a→b against rect by the Liang–Barsky
// algorithm, returning the surviving endpoints and whether any portion lies
// inside. Boundary contact counts as inside.
func clipSegmentToRect(a, b geom.Point, rect geom.BBox) (geom.Point, geom.Point, bool) {
	dx, dy := b.X-a.X, b.Y-a.Y
	t0, t1 := 0.0, 1.0
	edges := [4][2]float64{
		{-dx, a.X - rect.Min.X}, // left
		{dx, rect.Max.X - a.X},  // right
		{-dy, a.Y - rect.Min.Y}, // bottom
		{dy, rect.Max.Y - a.Y},  // top
	}
	for _, e := range edges {
		p, q := e[0], e[1]
		if p == 0 {
			if q < 0 {
				return geom.Point{}, geom.Point{}, false // parallel to and outside this edge
			}
			continue
		}
		r := q / p
		if p < 0 {
			if r > t1 {
				return geom.Point{}, geom.Point{}, false
			}
			if r > t0 {
				t0 = r
			}
			continue
		}
		if r < t0 {
			return geom.Point{}, geom.Point{}, false
		}
		if r < t1 {
			t1 = r
		}
	}
	p0 := geom.Point{X: a.X + t0*dx, Y: a.Y + t0*dy}
	p1 := geom.Point{X: a.X + t1*dx, Y: a.Y + t1*dy}
	return p0, p1, true
}
