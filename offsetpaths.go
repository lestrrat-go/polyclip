package polyclip

import (
	"errors"
	"math"
)

// ErrOffsetEndType is returned by [OffsetPaths] when given an end type that is
// not valid for open paths (currently only [EndPolygon]).
var ErrOffsetEndType = errors.New("polyclip: invalid open-path end type")

// OffsetPaths offsets open polylines into closed ribbon polygons: each input
// path is thickened by |d| on each side (total width 2·|d|) and its ends are
// capped per opts.End. The result is the Minkowski sum of the path with a disk
// of radius |d| for [EndRound]; [EndButt] and [EndSquare] use flat ends. Unlike
// [Offset], d is treated as a half-width: its magnitude sets the ribbon width
// and its sign is irrelevant.
//
// Each path needs at least two distinct points; shorter paths are skipped. The
// interior corners use opts.Join (see [JoinType]) exactly as [Offset] does, so
// the same miter/round/square/bevel geometry applies. Overlapping ribbons and
// the self-overlap of sharp interior turns are resolved by the same
// positive-fill self-union [Offset] uses (DESIGN.md §7.1).
//
// opts.End must be one of [EndButt] (the default), [EndSquare], [EndRound], or
// [EndJoined]; [EndPolygon] returns [ErrOffsetEndType]. [EndJoined] closes each
// path into a loop and bands it on both sides (see [EndJoined]). If every path
// is too short or the result is empty, OffsetPaths returns [ErrOffsetEmpty].
func OffsetPaths(lines []Polyline, d float64, opts OffsetOptions) (MultiPolygon, error) {
	if opts.End == EndPolygon {
		// EndPolygon is the closed-input behaviour of Offset; not a cap.
		return nil, ErrOffsetEndType
	}
	if len(lines) == 0 {
		return nil, ErrOffsetEmpty
	}
	w := math.Abs(d)
	if w == 0 {
		return nil, ErrOffsetEmpty
	}
	if opts.MiterLimit <= 0 {
		opts.MiterLimit = 2.0
	}
	if opts.ArcTol <= 0 {
		opts.ArcTol = w * 0.01
	}

	result := MultiPolygon{}
	for _, line := range lines {
		if opts.End == EndJoined {
			result = append(result, offsetJoinedBand(line, w, opts)...)
			continue
		}
		ring := offsetRibbon(line, w, opts)
		if len(ring) < 3 {
			continue
		}
		result = append(result, resolveOffsetPiece([]Polygon{ring})...)
	}
	if len(result) == 0 {
		return nil, ErrOffsetEmpty
	}
	return result, nil
}

// offsetRibbon builds the raw closed offset ring around one open polyline,
// offsetting w to each side. The ring is traced as one closed loop: the start
// cap, the forward side (interior joins), the end cap, then the reverse side.
// The result is CCW (positive winding inside) regardless of path direction, so
// it feeds the positive-fill self-union directly; interior turns that
// self-overlap are cleaned up there. Mirrors Clipper2's OffsetOpenPath.
func offsetRibbon(line Polyline, w float64, opts OffsetOptions) Polygon {
	pts := dedupPolyline(line)
	n := len(pts)
	if n < 2 {
		return nil
	}
	// Right-hand unit normals of each edge pts[i]→pts[i+1] (n-1 of them).
	norms := make([]Point, n-1)
	for i := range n - 1 {
		dir := pts[i+1].Sub(pts[i])
		l := dir.Len()
		norms[i] = Point{X: dir.Y / l, Y: -dir.X / l}
	}

	out := make(Polygon, 0, 2*n+8)
	// Start cap at pts[0] using the first edge's normal.
	emitEndCap(&out, pts[0], norms[0], w, opts)
	// Forward side: interior vertices, prev edge normal then next edge normal.
	for j := 1; j < n-1; j++ {
		emitVertex(&out, pts[j], norms[j-1], norms[j], w, opts)
	}
	// End cap at the last vertex using the last edge's normal, negated (the
	// reverse side runs along the opposite normals).
	last := norms[n-2].Neg()
	emitEndCap(&out, pts[n-1], last, w, opts)
	// Reverse side: interior vertices walked back with negated normals.
	for j := n - 2; j >= 1; j-- {
		emitVertex(&out, pts[j], norms[j].Neg(), norms[j-1].Neg(), w, opts)
	}
	return out
}

// offsetJoinedBand builds the band for one polyline under [EndJoined]: the
// polyline is closed into a loop (an implicit edge from its last vertex back to
// its first) and offset w to each side, producing the loop outline as a band.
// The outer boundary is the loop offset outward by w; the inner boundary is the
// loop offset inward by w, reversed so it reads as a hole. resolveOffsetPiece
// then emits an annulus when the loop encloses more than 2w, or a solid ribbon
// (the inner ring collapses / crosses out) otherwise. Every corner — including
// the former endpoints, now interior to the loop — uses opts.Join exactly as
// [Offset] does. Mirrors Clipper2's EndType::Joined.
func offsetJoinedBand(line Polyline, w float64, opts OffsetOptions) MultiPolygon {
	pts := dedupPolyline(line)
	for len(pts) >= 2 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1] // drop a closing duplicate; the loop is implicit
	}
	if len(pts) < 3 {
		// Fewer than three distinct vertices cannot form a loop with area; fall
		// back to the capped ribbon so a 2-point joined path still yields a band.
		ring := offsetRibbon(line, w, opts)
		if len(ring) < 3 {
			return nil
		}
		return resolveOffsetPiece([]Polygon{ring})
	}
	loop := Polygon(pts)
	if loop.SignedArea() < 0 {
		loop.Reverse() // orient CCW so +w grows outward, -w shrinks inward
	}
	outer := offsetRing(loop, w, opts)
	if len(outer) < 3 {
		return nil
	}
	rings := []Polygon{outer}
	if inner := offsetRing(loop, -w, opts); len(inner) >= 3 {
		inner.Reverse() // CW so it contributes negative winding (a hole)
		rings = append(rings, inner)
	}
	return resolveOffsetPiece(rings)
}

// emitEndCap appends the cap points for an open-path endpoint v. m is the
// relevant edge's right-hand normal oriented so that the outward path tangent
// is (m.Y, -m.X). The cap connects the two side offsets a = v−w·m and
// c = v+w·m, per opts.End. Mirrors Clipper2's DoBevel/DoSquare/DoRound for the
// j==k (endpoint) case.
func emitEndCap(out *Polygon, v, m Point, w float64, opts OffsetOptions) {
	a := Point{X: v.X - w*m.X, Y: v.Y - w*m.Y}
	c := Point{X: v.X + w*m.X, Y: v.Y + w*m.Y}
	switch opts.End {
	case EndRound:
		// Semicircle from a to c bulging outward; reuse the convex-join arc
		// (d>0 sweeps the 180° gap CCW, which passes through the outward side).
		appendRoundJoin(out, v, a, c, w, opts.ArcTol)
	case EndSquare:
		// Extend both side offsets |w| along the outward tangent and square off.
		t := Point{X: m.Y, Y: -m.X}
		*out = append(*out,
			Point{X: a.X + w*t.X, Y: a.Y + w*t.Y},
			Point{X: c.X + w*t.X, Y: c.Y + w*t.Y})
	default: // EndButt
		*out = append(*out, a, c)
	}
}

// dedupPolyline returns line with consecutive duplicate points removed.
func dedupPolyline(line Polyline) []Point {
	out := make([]Point, 0, len(line))
	for _, p := range line {
		if len(out) > 0 && out[len(out)-1] == p {
			continue
		}
		out = append(out, p)
	}
	return out
}
