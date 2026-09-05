// Package geom holds the value types that describe geometry — points,
// bounding boxes, rings, polygons, and open paths — together with the
// intrinsic queries and construction helpers that operate on them. It carries
// no dependency on the clipping engine; the boolean and offset operations live
// one layer up in package polyclip, which consumes these types.
package geom

import "math"

// Point is a 2D point in user units, optionally carrying a Z coordinate. The
// library does not interpret the units; the same units are used throughout an
// operation.
//
// Z is auxiliary data the geometry engine never reads — boolean ops, offset,
// and every other routine compare and snap points by X/Y only. Z is preserved
// from input to output and, when a ZAssigner is installed via
// Builder.SetZAssigner, computed for the new vertices created where edges
// cross (DESIGN.md §7.8h). Leave it zero if unused.
type Point struct {
	X, Y float64
	Z    float64
}

// The vector operations below — Sub, Add, Neg and Scale — read X and Y and
// leave Z zero in the result. Z is auxiliary data attached to a location, not a
// third coordinate the geometry means, so carrying it through an arithmetic
// result would assert a value the operation did not compute. Read Z off the
// operand where you need it.

// Sub returns the vector p - q.
func (p Point) Sub(q Point) Point {
	return Point{X: p.X - q.X, Y: p.Y - q.Y}
}

// Add returns the vector p + q, which is p displaced by q.
func (p Point) Add(q Point) Point {
	return Point{X: p.X + q.X, Y: p.Y + q.Y}
}

// Neg returns the vector -p.
func (p Point) Neg() Point {
	return Point{X: -p.X, Y: -p.Y}
}

// Scale returns p multiplied by s.
func (p Point) Scale(s float64) Point {
	return Point{X: p.X * s, Y: p.Y * s}
}

// Cross returns the 2D cross product p × q (p.X*q.Y - p.Y*q.X). Treating p and
// q as vectors, its sign gives their turn orientation and its magnitude is the
// area of the parallelogram they span.
func (p Point) Cross(q Point) float64 {
	return p.X*q.Y - p.Y*q.X
}

// Dot returns the dot product p · q.
func (p Point) Dot(q Point) float64 {
	return p.X*q.X + p.Y*q.Y
}

// Dist2 returns the squared Euclidean distance between p and q. Squared
// distance avoids the square root when only comparisons are needed.
func (p Point) Dist2(q Point) float64 {
	dx, dy := p.X-q.X, p.Y-q.Y
	return dx*dx + dy*dy
}

// Dist returns the Euclidean distance between p and q. It goes through
// [math.Hypot], so it is accurate for operands whose squares would overflow or
// underflow; use [Point.Dist2] where only comparisons are needed and the square
// root is waste.
func (p Point) Dist(q Point) float64 {
	return math.Hypot(p.X-q.X, p.Y-q.Y)
}

// Len returns the Euclidean magnitude of p treated as a vector from the origin.
func (p Point) Len() float64 {
	return math.Hypot(p.X, p.Y)
}

// Normalize returns p scaled to unit length, and whether there was a direction
// to return.
//
// It reports false — and a zero Point — for a p that has no direction: the zero
// vector, one whose coordinates are not finite, and one so small that its length
// underflows to zero. A caller that ignored the bool would otherwise get a NaN
// or an infinity in the shape of a perfectly ordinary unit vector, which is the
// one result worth refusing here.
//
// The length is [math.Hypot], so a vector whose squared length would overflow —
// (1e300, 1e300) — normalizes correctly rather than to a NaN.
func (p Point) Normalize() (Point, bool) {
	l := p.Len()
	if l == 0 || math.IsInf(l, 0) || math.IsNaN(l) {
		return Point{}, false
	}
	return Point{X: p.X / l, Y: p.Y / l}, true
}

// Equal reports whether p and q are within tol of each other, measured as the
// Euclidean distance between them.
//
// A tol of 0 asks whether the two are the same point exactly, which is the right
// question only for coordinates that came from the same computation. Anything
// derived through different arithmetic wants a tolerance. A negative or NaN tol
// is no bound on a distance and admits nothing.
//
// Only X and Y are compared. Z is auxiliary data attached to a location, not
// part of the location being judged.
//
// A point whose X or Y is not finite is not a location, and is equal to nothing —
// itself included, and at every tol, +Inf among them. There is no real distance
// for a tolerance to bound, and the alternative is that an infinite tol quietly
// declares two infinities the same place.
func (p Point) Equal(q Point, tol float64) bool {
	if !(tol >= 0) || !p.finite() || !q.finite() {
		return false
	}
	return p.Dist(q) <= tol
}

// finite reports whether p names a location: both coordinates real numbers.
func (p Point) finite() bool {
	return !math.IsInf(p.X, 0) && !math.IsNaN(p.X) && !math.IsInf(p.Y, 0) && !math.IsNaN(p.Y)
}

// BBox is an axis-aligned bounding box. The zero value represents an empty
// box; callers should use [BBox.Empty] to check this rather than comparing to
// the zero value directly.
type BBox struct {
	Min, Max Point
}

// Empty reports whether b is the empty bounding box.
//
// An empty box has Min strictly greater than Max on at least one axis. The
// zero [BBox] is not empty by this definition (it represents the single point
// at the origin); use [EmptyBBox] when you need a sentinel empty box.
func (b BBox) Empty() bool {
	return b.Min.X > b.Max.X || b.Min.Y > b.Max.Y
}

// EmptyBBox returns a bounding box that reports true from [BBox.Empty] and
// expands cleanly when extended via [BBox.Add] or [BBox.Union].
func EmptyBBox() BBox {
	return BBox{
		Min: Point{X: math.Inf(+1), Y: math.Inf(+1)},
		Max: Point{X: math.Inf(-1), Y: math.Inf(-1)},
	}
}

// Add returns the smallest bounding box containing both b and p.
func (b BBox) Add(p Point) BBox {
	if b.Empty() {
		return BBox{Min: p, Max: p}
	}
	return BBox{
		Min: Point{X: min(b.Min.X, p.X), Y: min(b.Min.Y, p.Y)},
		Max: Point{X: max(b.Max.X, p.X), Y: max(b.Max.Y, p.Y)},
	}
}

// Union returns the smallest bounding box containing both b and other.
// An empty operand is ignored.
func (b BBox) Union(other BBox) BBox {
	switch {
	case b.Empty():
		return other
	case other.Empty():
		return b
	}
	return BBox{
		Min: Point{X: min(b.Min.X, other.Min.X), Y: min(b.Min.Y, other.Min.Y)},
		Max: Point{X: max(b.Max.X, other.Max.X), Y: max(b.Max.Y, other.Max.Y)},
	}
}

// Contains reports whether p lies within b. Points on the boundary count as
// inside.
func (b BBox) Contains(p Point) bool {
	if b.Empty() {
		return false
	}
	return p.X >= b.Min.X && p.X <= b.Max.X && p.Y >= b.Min.Y && p.Y <= b.Max.Y
}

// Intersects reports whether b and other share at least one point, including
// boundary contact. An empty box intersects nothing.
func (b BBox) Intersects(other BBox) bool {
	if b.Empty() || other.Empty() {
		return false
	}
	return b.Min.X <= other.Max.X && b.Max.X >= other.Min.X &&
		b.Min.Y <= other.Max.Y && b.Max.Y >= other.Min.Y
}

// Width returns b.Max.X - b.Min.X, or 0 if b is empty.
func (b BBox) Width() float64 {
	if b.Empty() {
		return 0
	}
	return b.Max.X - b.Min.X
}

// Height returns b.Max.Y - b.Min.Y, or 0 if b is empty.
func (b BBox) Height() float64 {
	if b.Empty() {
		return 0
	}
	return b.Max.Y - b.Min.Y
}
