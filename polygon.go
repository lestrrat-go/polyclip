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
// This invariant is not checked at construction; pass an [ExPolygon] through
// a boolean operation (e.g. [Union] with itself) if you need it normalized.
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
