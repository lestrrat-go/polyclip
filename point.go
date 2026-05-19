package polyclip

import "math"

// Point is a 2D point in user units. The library does not interpret the
// units; the same units are used throughout an operation.
type Point struct {
	X, Y float64
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
		Min: Point{X: math.Min(b.Min.X, p.X), Y: math.Min(b.Min.Y, p.Y)},
		Max: Point{X: math.Max(b.Max.X, p.X), Y: math.Max(b.Max.Y, p.Y)},
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
		Min: Point{X: math.Min(b.Min.X, other.Min.X), Y: math.Min(b.Min.Y, other.Min.Y)},
		Max: Point{X: math.Max(b.Max.X, other.Max.X), Y: math.Max(b.Max.Y, other.Max.Y)},
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
