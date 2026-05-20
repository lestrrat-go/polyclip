package polyclip

import (
	"fmt"
	"math"
)

// IssueKind enumerates the structural problems [MultiPolygon.Validate]
// can report. It is not an error type; validation issues are
// diagnostics — they don't prevent the boolean engine from running,
// but they may indicate that the result will be surprising.
type IssueKind int

const (
	// IssueTooFewVertices marks a ring with fewer than three points.
	// Such a "ring" has no interior and is silently dropped by the
	// boolean engine.
	IssueTooFewVertices IssueKind = iota
	// IssueWrongWinding marks an outer ring with non-CCW orientation
	// (signed area ≤ 0) or a hole ring with non-CW orientation
	// (signed area ≥ 0). The boolean engine accepts either winding for
	// outers — it normalises internally — but Offset and hole/outer
	// distinctions in the public API rely on the documented
	// convention.
	IssueWrongWinding
	// IssueSelfIntersecting marks a ring whose edges cross each other.
	// Self-intersecting input may still produce reasonable output from
	// the boolean engine (Union of a self-intersecting polygon resolves
	// it under the Vatti rules), but Offset's direct construction
	// assumes a simple ring.
	IssueSelfIntersecting
	// IssueHoleOutsideOuter marks a hole with at least one vertex
	// outside its containing outer ring.
	IssueHoleOutsideOuter
	// IssueHolesOverlap marks a pair of holes within the same
	// ExPolygon whose interiors overlap.
	IssueHolesOverlap
)

// String returns a short, lowercase tag for the issue kind, suitable
// for log lines and test failure messages.
func (k IssueKind) String() string {
	switch k {
	case IssueTooFewVertices:
		return "too-few-vertices"
	case IssueWrongWinding:
		return "wrong-winding"
	case IssueSelfIntersecting:
		return "self-intersecting"
	case IssueHoleOutsideOuter:
		return "hole-outside-outer"
	case IssueHolesOverlap:
		return "holes-overlap"
	}
	return fmt.Sprintf("unknown(%d)", int(k))
}

// ValidationIssue locates one structural problem reported by
// [MultiPolygon.Validate]. Ring is -1 for the outer ring of
// ExIdx, otherwise the index into ExIdx's Holes slice. Msg is a
// human-readable description.
type ValidationIssue struct {
	Kind  IssueKind
	ExIdx int
	Ring  int
	Msg   string
}

// String formats an issue as `<kind> @ ex[i] outer|hole[j]: <msg>`,
// suitable for inclusion in error logs or test failure output.
func (v ValidationIssue) String() string {
	ring := "outer"
	if v.Ring >= 0 {
		ring = fmt.Sprintf("hole[%d]", v.Ring)
	}
	return fmt.Sprintf("%s @ ex[%d] %s: %s", v.Kind, v.ExIdx, ring, v.Msg)
}

// Validate reports structural problems in m. The returned slice is
// non-nil only when at least one problem was found; valid input
// returns nil.
//
// Checks performed:
//
//   - [IssueTooFewVertices]: ring has fewer than three vertices.
//   - [IssueWrongWinding]: outer not CCW or hole not CW.
//   - [IssueSelfIntersecting]: ring's edges cross. O(n²) per ring.
//   - [IssueHoleOutsideOuter]: a hole vertex lies outside the outer
//     ring (full containment is required by the polyclip contract).
//   - [IssueHolesOverlap]: two holes' interiors overlap. The check is
//     conservative — it samples each hole's first vertex against the
//     other hole and looks for edge crossings.
//
// Validate is read-only and allocates only the issue slice; it does
// not mutate m.
func (m MultiPolygon) Validate() []ValidationIssue {
	var issues []ValidationIssue
	for i, ex := range m {
		// Outer ring.
		if len(ex.Outer) < 3 {
			issues = append(issues, ValidationIssue{
				Kind: IssueTooFewVertices, ExIdx: i, Ring: -1,
				Msg: fmt.Sprintf("%d vertices", len(ex.Outer)),
			})
			// Skip further outer checks; we'd misclassify everything.
			continue
		}
		if area := ex.Outer.SignedArea(); area <= 0 {
			issues = append(issues, ValidationIssue{
				Kind: IssueWrongWinding, ExIdx: i, Ring: -1,
				Msg: fmt.Sprintf("outer signed area %v (want > 0 for CCW)", area),
			})
		}
		if idx1, idx2, ok := ringSelfIntersection(ex.Outer); ok {
			issues = append(issues, ValidationIssue{
				Kind: IssueSelfIntersecting, ExIdx: i, Ring: -1,
				Msg: fmt.Sprintf("edges %d and %d cross", idx1, idx2),
			})
		}

		// Holes.
		for hi, h := range ex.Holes {
			if len(h) < 3 {
				issues = append(issues, ValidationIssue{
					Kind: IssueTooFewVertices, ExIdx: i, Ring: hi,
					Msg: fmt.Sprintf("%d vertices", len(h)),
				})
				continue
			}
			if area := h.SignedArea(); area >= 0 {
				issues = append(issues, ValidationIssue{
					Kind: IssueWrongWinding, ExIdx: i, Ring: hi,
					Msg: fmt.Sprintf("hole signed area %v (want < 0 for CW)", area),
				})
			}
			if idx1, idx2, ok := ringSelfIntersection(h); ok {
				issues = append(issues, ValidationIssue{
					Kind: IssueSelfIntersecting, ExIdx: i, Ring: hi,
					Msg: fmt.Sprintf("edges %d and %d cross", idx1, idx2),
				})
			}
			if !holeInsideOuter(h, ex.Outer) {
				issues = append(issues, ValidationIssue{
					Kind: IssueHoleOutsideOuter, ExIdx: i, Ring: hi,
					Msg: "at least one vertex outside outer",
				})
			}
			for hj := range hi {
				if ringsOverlap(h, ex.Holes[hj]) {
					issues = append(issues, ValidationIssue{
						Kind: IssueHolesOverlap, ExIdx: i, Ring: hi,
						Msg: fmt.Sprintf("overlaps hole[%d]", hj),
					})
				}
			}
		}
	}
	return issues
}

// ringSelfIntersection reports the first pair of non-adjacent edges of
// p that strictly cross. Adjacent edges (sharing an endpoint) are
// skipped; coincident vertices on touching but not crossing edges are
// also tolerated. O(n²) in the number of edges; intended for
// validation, not hot paths.
func ringSelfIntersection(p Polygon) (int, int, bool) {
	n := len(p)
	if n < 4 {
		return 0, 0, false
	}
	for i := range n {
		a1, a2 := p[i], p[(i+1)%n]
		for j := i + 2; j < n; j++ {
			// Skip the edge adjacent to edge i across the ring wrap-around.
			if i == 0 && j == n-1 {
				continue
			}
			b1, b2 := p[j], p[(j+1)%n]
			if segmentsCrossStrict(a1, a2, b1, b2) {
				return i, j, true
			}
		}
	}
	return 0, 0, false
}

// segmentsCrossStrict reports whether segments a1a2 and b1b2 cross in
// their interiors (proper crossing). Touch-but-no-cross at endpoints
// is not a crossing; collinear overlap is not a crossing.
func segmentsCrossStrict(a1, a2, b1, b2 Point) bool {
	d1 := triOrient(b1, b2, a1)
	d2 := triOrient(b1, b2, a2)
	d3 := triOrient(a1, a2, b1)
	d4 := triOrient(a1, a2, b2)
	return d1*d2 < 0 && d3*d4 < 0
}

// triOrient returns positive when (a, b, c) makes a left turn,
// negative for a right turn, and 0 when collinear.
func triOrient(a, b, c Point) float64 {
	v := (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

// holeInsideOuter reports whether every vertex of hole lies in outer.
// A hole that crosses the outer boundary will have at least one vertex
// outside (with very few pathological exceptions involving the hole
// snaking along the outer boundary; those also trip the
// self-intersection or overlap checks).
func holeInsideOuter(hole, outer Polygon) bool {
	for _, v := range hole {
		if !outer.Contains(v) {
			return false
		}
	}
	return true
}

// ringsOverlap reports whether the interiors of two simple closed
// rings overlap. Approximation: returns true if either ring contains
// a vertex of the other (catches nested rings), or if any edge of one
// strictly crosses any edge of the other (catches partial overlap).
// Touching at a vertex with no shared interior returns false.
func ringsOverlap(a, b Polygon) bool {
	if len(a) < 3 || len(b) < 3 {
		return false
	}
	// Vertex-in-other-ring: pick a strictly-interior sample (centroid of
	// the first three points) instead of vertex[0] to avoid coincident-
	// boundary false negatives.
	if pointStrictlyInside(ringSample(a), b) {
		return true
	}
	if pointStrictlyInside(ringSample(b), a) {
		return true
	}
	// Edge-edge proper crossing.
	for i := range a {
		a1, a2 := a[i], a[(i+1)%len(a)]
		for j := range b {
			b1, b2 := b[j], b[(j+1)%len(b)]
			if segmentsCrossStrict(a1, a2, b1, b2) {
				return true
			}
		}
	}
	return false
}

// ringSample returns a point likely to be strictly inside ring,
// computed as the centroid of its first three vertices. For convex
// rings this is always interior; for concave rings a centroid-of-three
// may fall outside but still suffices as a sampling heuristic.
func ringSample(ring Polygon) Point {
	a, b, c := ring[0], ring[1], ring[2]
	return Point{X: (a.X + b.X + c.X) / 3, Y: (a.Y + b.Y + c.Y) / 3}
}

// pointStrictlyInside is [Polygon.Contains] but returns false when q
// lies on the boundary (within a small tolerance). Used by
// [ringsOverlap] so that holes that touch at a vertex but don't share
// interior are not flagged.
func pointStrictlyInside(q Point, p Polygon) bool {
	if !p.Contains(q) {
		return false
	}
	if pointOnRingBoundary(p, q) {
		return false
	}
	// p.Contains can return true with q exactly on an edge; we already
	// filtered that above. As a belt-and-braces check, ensure q is
	// strictly inside the bounding box.
	bb := p.BoundingBox()
	const eps = 1e-12
	if math.Abs(q.X-bb.Min.X) < eps || math.Abs(q.X-bb.Max.X) < eps ||
		math.Abs(q.Y-bb.Min.Y) < eps || math.Abs(q.Y-bb.Max.Y) < eps {
		return false
	}
	return true
}
