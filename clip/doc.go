// Package clip is the scanline boolean engine that powers polyclip's Union,
// Intersect, Difference and Xor operations.
//
// The engine implements the Vatti polygon clipping algorithm on a fixed-point
// integer grid (see [github.com/lestrrat-go/polyclip/fixed]). The high-level
// flow is:
//
//  1. The caller converts user-space polygons to fixed-point [Segment]s
//     tagged by [Source].
//  2. Segments are fed through the scanline [Sweep], which maintains an
//     active edge list and emits intersection events.
//  3. A classification table decides which edge contributions belong to the
//     output, based on the boolean operation and the running winding counts
//     of the subject and clip inputs.
//  4. The accumulated contributions are reassembled into output polygons.
//
// Public symbols here are exported so other subpackages within this module
// can address them in tests; they are not part of polyclip's stable API.
package clip
