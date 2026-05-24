package polyclip

import "math"

// Polygon is a simple closed ring of points. The closing edge from the last
// point back to the first is implicit; do not duplicate the first point at
// the end of the slice.
//
// Polygon does not enforce simplicity or any particular winding on its own —
// boolean and offset operations normalize input as part of their internal
// preprocessing. Use [Polygon.IsCCW] to check winding and [Polygon.Reverse]
// to flip it in place.
type Polygon []Point

// ExPolygon ("extended polygon") is a simple polygon with zero or more holes
// nested inside it. The library treats the outer ring's interior minus the
// union of the holes' interiors as the represented region.
//
// Holes must be fully contained in Outer and must not overlap each other.
// This invariant is not checked at construction; pass an [ExPolygon] (wrapped
// in a [MultiPolygon]) through [Simplify] if you need it normalized.
type ExPolygon struct {
	Outer Polygon
	Holes []Polygon
}

// MultiPolygon is a disjoint union of [ExPolygon] values. It is the closed
// type for every boolean and offset operation — operations may produce
// zero, one, or many output [ExPolygon] values from any given input.
type MultiPolygon []ExPolygon

// SignedArea returns twice the signed area of the polygon's interior using
// the shoelace formula. It is positive for counter-clockwise rings and
// negative for clockwise rings, given the conventional screen orientation
// where Y increases upward.
//
// For polygons with fewer than 3 points it returns 0.
func (p Polygon) SignedArea() float64 {
	n := len(p)
	if n < 3 {
		return 0
	}
	var s float64
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		s += p[i].X*p[j].Y - p[j].X*p[i].Y
	}
	return s / 2
}

// Area returns the absolute area enclosed by the ring.
func (p Polygon) Area() float64 {
	return math.Abs(p.SignedArea())
}

// IsCCW reports whether the polygon is wound counter-clockwise.
//
// A degenerate polygon (fewer than 3 distinct points, or zero signed area)
// reports false.
func (p Polygon) IsCCW() bool {
	return p.SignedArea() > 0
}

// Reverse flips the winding of the polygon in place.
func (p Polygon) Reverse() {
	for i, j := 0, len(p)-1; i < j; i, j = i+1, j-1 {
		p[i], p[j] = p[j], p[i]
	}
}

// BoundingBox returns the axis-aligned box containing every vertex.
// An empty polygon returns the empty bounding box from [EmptyBBox].
func (p Polygon) BoundingBox() BBox {
	if len(p) == 0 {
		return EmptyBBox()
	}
	b := BBox{Min: p[0], Max: p[0]}
	for _, q := range p[1:] {
		b = b.Add(q)
	}
	return b
}

// BoundingBox returns the axis-aligned box containing the outer ring.
// Holes never extend outside Outer, so they are not consulted.
func (e ExPolygon) BoundingBox() BBox {
	return e.Outer.BoundingBox()
}

// BoundingBox returns the axis-aligned box containing every ExPolygon.
func (m MultiPolygon) BoundingBox() BBox {
	b := EmptyBBox()
	for i := range m {
		b = b.Union(m[i].BoundingBox())
	}
	return b
}

// Area returns the absolute area of the region: Outer minus the sum of
// hole areas.
func (e ExPolygon) Area() float64 {
	a := e.Outer.Area()
	for i := range e.Holes {
		a -= e.Holes[i].Area()
	}
	if a < 0 {
		return 0
	}
	return a
}

// Area returns the total area of the multipolygon.
func (m MultiPolygon) Area() float64 {
	var a float64
	for i := range m {
		a += m[i].Area()
	}
	return a
}

// Contains reports whether q lies inside p using the even-odd rule. Points
// exactly on the boundary count as inside.
func (p Polygon) Contains(q Point) bool {
	n := len(p)
	if n < 3 {
		return false
	}
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		if pointOnSegment(p[i], p[j], q) {
			return true
		}
	}
	inside := false
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		yi, yj := p[i].Y, p[j].Y
		if (yi > q.Y) != (yj > q.Y) {
			xCross := (p[j].X-p[i].X)*(q.Y-yi)/(yj-yi) + p[i].X
			if q.X < xCross {
				inside = !inside
			}
		}
	}
	return inside
}

// Contains reports whether q lies inside the ExPolygon — inside Outer and
// not strictly inside any hole. Points on Outer's boundary or on a hole's
// boundary count as inside.
func (e ExPolygon) Contains(q Point) bool {
	if !e.Outer.Contains(q) {
		return false
	}
	for i := range e.Holes {
		hole := e.Holes[i]
		if !hole.Contains(q) {
			continue
		}
		if pointOnRingBoundary(hole, q) {
			return true
		}
		return false
	}
	return true
}

// Contains reports whether q lies inside any [ExPolygon] of m.
func (m MultiPolygon) Contains(q Point) bool {
	for i := range m {
		if m[i].Contains(q) {
			return true
		}
	}
	return false
}

// pointOnSegment reports whether q lies on the closed segment ab.
//
// Uses the standard collinearity-and-bounds check. The cross product is
// computed in float64; for inputs within typical user-coordinate ranges this
// is sufficient for Phase 0 utility methods. The boolean engine uses exact
// integer predicates instead (see DESIGN.md §5.2).
func pointOnSegment(a, b, q Point) bool {
	cross := (b.X-a.X)*(q.Y-a.Y) - (b.Y-a.Y)*(q.X-a.X)
	if math.Abs(cross) > pointOnSegmentEpsilon(a, b) {
		return false
	}
	if q.X < math.Min(a.X, b.X) || q.X > math.Max(a.X, b.X) {
		return false
	}
	if q.Y < math.Min(a.Y, b.Y) || q.Y > math.Max(a.Y, b.Y) {
		return false
	}
	return true
}

// pointOnSegmentEpsilon scales the collinearity tolerance with the segment's
// magnitude so the test is invariant under uniform scaling of the input.
func pointOnSegmentEpsilon(a, b Point) float64 {
	scale := math.Max(math.Max(math.Abs(a.X), math.Abs(a.Y)), math.Max(math.Abs(b.X), math.Abs(b.Y)))
	const eps = 1e-12
	if scale < 1 {
		return eps
	}
	return eps * scale * scale
}

// Clean returns a copy of m with cosmetic artifacts removed:
//
//   - Consecutive vertices closer than vertexTol are deduplicated. The
//     check wraps around: a final vertex that snaps onto the first is
//     dropped too.
//   - Interior vertices whose perpendicular distance from the line
//     joining their immediate neighbours is at most vertexTol are
//     removed. The pass repeats until stable, so chains of collinear
//     vertices collapse to a single edge.
//   - Rings whose absolute signed area is strictly less than minArea
//     are dropped. If an ExPolygon's outer ring drops, the entire
//     piece is omitted; a dropped hole leaves the outer intact.
//
// vertexTol and minArea must be non-negative; pass zero to disable
// either check (with vertexTol=0 only exact duplicates are merged).
// Clean is purely geometric — it does not run the boolean engine and
// cannot resolve self-intersection. For that, use [Simplify].
func (m MultiPolygon) Clean(vertexTol, minArea float64) MultiPolygon {
	out := make(MultiPolygon, 0, len(m))
	for _, ex := range m {
		outer := cleanRing(ex.Outer, vertexTol)
		if outer == nil || math.Abs(outer.SignedArea()) < minArea {
			continue
		}
		cleaned := ExPolygon{Outer: outer}
		for _, h := range ex.Holes {
			holeC := cleanRing(h, vertexTol)
			if holeC == nil || math.Abs(holeC.SignedArea()) < minArea {
				continue
			}
			cleaned.Holes = append(cleaned.Holes, holeC)
		}
		out = append(out, cleaned)
	}
	return out
}

// cleanRing applies the consecutive-duplicate and collinear-vertex
// passes from [MultiPolygon.Clean] to a single ring, returning nil if
// fewer than 3 vertices remain.
func cleanRing(ring Polygon, vertexTol float64) Polygon {
	if len(ring) < 3 {
		return nil
	}
	tol2 := vertexTol * vertexTol
	deduped := make(Polygon, 0, len(ring))
	for _, v := range ring {
		if n := len(deduped); n > 0 {
			last := deduped[n-1]
			if dx, dy := v.X-last.X, v.Y-last.Y; dx*dx+dy*dy <= tol2 {
				continue
			}
		}
		deduped = append(deduped, v)
	}
	// Wrap-around dedup: collapse trailing copies of the first vertex.
	for len(deduped) >= 2 {
		first := deduped[0]
		last := deduped[len(deduped)-1]
		dx, dy := first.X-last.X, first.Y-last.Y
		if dx*dx+dy*dy > tol2 {
			break
		}
		deduped = deduped[:len(deduped)-1]
	}
	if len(deduped) < 3 {
		return nil
	}
	// Iterate collinear removal until stable. One pass isn't enough: when
	// a vertex is dropped its former neighbours may themselves become
	// collinear with their new neighbours.
	for {
		n := len(deduped)
		kept := make(Polygon, 0, n)
		removed := false
		for i := range n {
			prev := deduped[(i-1+n)%n]
			next := deduped[(i+1)%n]
			if pointCollinear(prev, deduped[i], next, vertexTol) {
				removed = true
				continue
			}
			kept = append(kept, deduped[i])
		}
		deduped = kept
		if !removed || len(deduped) < 3 {
			break
		}
	}
	if len(deduped) < 3 {
		return nil
	}
	return deduped
}

// pointCollinear reports whether v lies within tol of the line through
// a and b. When a == b, returns true only if v also coincides with that
// point (within tol).
func pointCollinear(a, v, b Point, tol float64) bool {
	abx := b.X - a.X
	aby := b.Y - a.Y
	len2 := abx*abx + aby*aby
	if len2 == 0 {
		dx, dy := v.X-a.X, v.Y-a.Y
		return dx*dx+dy*dy <= tol*tol
	}
	cross := abx*(v.Y-a.Y) - aby*(v.X-a.X)
	return cross*cross <= tol*tol*len2
}

func pointOnRingBoundary(p Polygon, q Point) bool {
	n := len(p)
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		if pointOnSegment(p[i], p[j], q) {
			return true
		}
	}
	return false
}
