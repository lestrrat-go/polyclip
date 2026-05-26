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

// Sub returns the vector p - q.
func (p Point) Sub(q Point) Point {
	return Point{X: p.X - q.X, Y: p.Y - q.Y}
}

// Neg returns the vector -p.
func (p Point) Neg() Point {
	return Point{X: -p.X, Y: -p.Y}
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

// Len returns the Euclidean magnitude of p treated as a vector from the origin.
func (p Point) Len() float64 {
	return math.Hypot(p.X, p.Y)
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
