// Package polyclip is a pure-Go library for 2D polygon boolean operations
// (union, intersection, difference, symmetric difference) and polygon offset
// (Minkowski sum / erosion with a disk).
//
// The "shape" primitive is a simple-polygon-with-holes ([ExPolygon]). A
// collection of disjoint [ExPolygon] values is a [MultiPolygon], which is the
// closed type that every boolean and offset operation consumes and produces.
//
// All public operations accept [MultiPolygon] in and return [MultiPolygon]
// out — there is no separate "post-processing" step the caller has to run on
// the result to make it usable.
//
// Inputs are taken in user units as [float64]. Internally the engine works on
// a fixed-point integer grid for numeric robustness; see DESIGN.md §5 for
// details.
//
// # Conventions
//
//   - Outer rings are normalized to counter-clockwise winding and holes to
//     clockwise winding, but both orientations are accepted on input.
//   - The closing edge of every ring is implicit: do not repeat the first
//     point at the end of the slice.
//   - Boundary points are considered to be inside the polygon by the
//     even-odd rule used in [Polygon.Contains].
//
// # Stability
//
// The only stable public API surface is what is exported from this top-level
// package. Subpackages under this module (e.g. clip/, offset/, fixed/) are
// implementation detail and may change without notice.
package polyclip
