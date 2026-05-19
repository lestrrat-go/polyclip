package polyclip

import (
	"errors"
	"fmt"

	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// ErrHorizontalNotSupported is returned by [Union] (and future boolean ops)
// when an input contains a horizontal edge that the engine cannot classify
// as a local-min or local-max horizontal (typically a mid-bound horizontal
// in a staircase). Axial rectangles and other inputs whose horizontals are
// all local-min/max are supported; mid-bound horizontals are deferred to a
// later increment (DESIGN.md §12.6 / §12.8 increment 4').
var ErrHorizontalNotSupported = errors.New("polyclip: input contains a horizontal edge that is neither a local minimum nor a local maximum of its ring")

// Union returns a ∪ b.
//
// Handled cases:
//
//   - Empty inputs: Union(empty, b) returns b unchanged, Union(a, empty)
//     returns a.
//   - Strictly disjoint bounding boxes: equivalent to concatenation. The
//     two MultiPolygons are returned spliced together with no engine work.
//   - Inputs with non-horizontal edges or with horizontal edges that are
//     each a local minimum (polygon bottom) or local maximum (polygon
//     top) of their ring: the Vatti engine in
//     [github.com/lestrrat-go/polyclip/clip] runs over the snapped
//     segments. Output rings are converted back to a float64
//     MultiPolygon. Hole assignment uses signed-area sign and bbox-prefilter
//     point-in-polygon (DESIGN.md §11.9).
//
// Inputs containing a mid-bound horizontal (a staircase step) return
// [ErrHorizontalNotSupported] when the bound-model pre-pass fails on
// shared-vertex inputs that fall back to the per-edge path.
func Union(a, b MultiPolygon) (MultiPolygon, error) {
	switch {
	case len(a) == 0 && len(b) == 0:
		return MultiPolygon{}, nil
	case len(a) == 0:
		return b, nil
	case len(b) == 0:
		return a, nil
	}

	if !a.BoundingBox().Intersects(b.BoundingBox()) {
		out := make(MultiPolygon, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out, nil
	}

	return runBooleanOp(a, b, clip.OpUnion)
}

// Intersect returns a ∩ b.
//
// Empty input or disjoint bounding boxes short-circuit to the empty
// MultiPolygon. Otherwise the Vatti engine runs with [clip.OpIntersect]
// and the §11.4 / §12.5 classification rules emit exactly the region
// covered by BOTH inputs.
func Intersect(a, b MultiPolygon) (MultiPolygon, error) {
	if len(a) == 0 || len(b) == 0 {
		return MultiPolygon{}, nil
	}
	if !a.BoundingBox().Intersects(b.BoundingBox()) {
		return MultiPolygon{}, nil
	}
	return runBooleanOp(a, b, clip.OpIntersect)
}

// Difference returns a ∖ b — the region covered by a but not by b.
//
// Empty subject (a) short-circuits to empty; empty clip (b) returns a
// unchanged. Disjoint bounding boxes return a unchanged. Otherwise the
// Vatti engine runs with [clip.OpDifference].
func Difference(a, b MultiPolygon) (MultiPolygon, error) {
	if len(a) == 0 {
		return MultiPolygon{}, nil
	}
	if len(b) == 0 {
		return a, nil
	}
	if !a.BoundingBox().Intersects(b.BoundingBox()) {
		return a, nil
	}
	return runBooleanOp(a, b, clip.OpDifference)
}

// Xor returns the symmetric difference (a ∪ b) ∖ (a ∩ b) — the region
// covered by exactly one of the inputs.
//
// Empty operands short-circuit to the other input (or empty if both are
// empty). Disjoint bounding boxes return the concatenation, equivalent to
// Union. Otherwise the Vatti engine runs with [clip.OpXor].
func Xor(a, b MultiPolygon) (MultiPolygon, error) {
	switch {
	case len(a) == 0 && len(b) == 0:
		return MultiPolygon{}, nil
	case len(a) == 0:
		return b, nil
	case len(b) == 0:
		return a, nil
	}
	if !a.BoundingBox().Intersects(b.BoundingBox()) {
		out := make(MultiPolygon, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out, nil
	}
	return runBooleanOp(a, b, clip.OpXor)
}

// runBooleanOp is the engine path: snap inputs to a fixed-point grid, feed
// segments through the sweep, and convert rings back to a user-space
// MultiPolygon.
func runBooleanOp(a, b MultiPolygon, op clip.Operation) (MultiPolygon, error) {
	bbox := a.BoundingBox().Union(b.BoundingBox())
	scale := fixed.ScaleFromBBox(bbox.Min.X, bbox.Min.Y, bbox.Max.X, bbox.Max.Y)

	segs := collectSegments(a, clip.Subject, scale)
	segs = append(segs, collectSegments(b, clip.Clip, scale)...)

	segs = clip.SplitOverlaps(segs)
	sw := clip.Sweep(segs, op)
	if sw.Err != nil {
		if errors.Is(sw.Err, clip.ErrUnsupportedHorizontal) {
			return nil, fmt.Errorf("%w: %v", ErrHorizontalNotSupported, sw.Err)
		}
		return nil, sw.Err
	}
	return assembleResult(sw.Rings, scale), nil
}

// collectSegments converts every input edge into a fixed-point Segment and
// returns the slice. Horizontal segments are kept; the engine classifies
// them in a pre-pass.
func collectSegments(m MultiPolygon, src clip.Source, scale fixed.Scale) []clip.Segment {
	var out []clip.Segment
	for _, ex := range m {
		appendRing(&out, ex.Outer, src, scale)
		for _, h := range ex.Holes {
			appendRing(&out, h, src, scale)
		}
	}
	return out
}

func appendRing(dst *[]clip.Segment, ring Polygon, src clip.Source, scale fixed.Scale) {
	n := len(ring)
	if n < 3 {
		return
	}
	for i := range n {
		j := i + 1
		if j == n {
			j = 0
		}
		a := scale.Snap(ring[i].X, ring[i].Y)
		b := scale.Snap(ring[j].X, ring[j].Y)
		seg := clip.NewSegment(a, b, src)
		if seg.Degenerate() {
			continue
		}
		*dst = append(*dst, seg)
	}
}

// assembleResult converts the sweep's closed output rings into a user-space
// MultiPolygon, classifying each ring as outer or hole by its signed area
// and grouping holes into their containing outer.
func assembleResult(rings []*clip.OutRec, scale fixed.Scale) MultiPolygon {
	type classified struct {
		poly Polygon
		bbox BBox
	}
	var outers []classified
	var holes []classified

	for _, r := range rings {
		if r.Pts == nil {
			continue
		}
		fixedPts := r.Points()
		if len(fixedPts) < 3 {
			continue
		}
		poly := make(Polygon, len(fixedPts))
		for i, fp := range fixedPts {
			poly[i].X, poly[i].Y = scale.Unsnap(fp)
		}
		c := classified{poly: poly, bbox: poly.BoundingBox()}
		if poly.SignedArea() > 0 {
			outers = append(outers, c)
		} else {
			holes = append(holes, c)
		}
	}

	// First pass: resolve hole→outer ownership. CW rings (negative signed
	// area) with no enclosing outer are not actually holes — they came out
	// of the sweep in CW direction (typical of Intersect / Difference /
	// Xor where the cycle's Front/Back assignment differs from Union's).
	// Reverse them and promote to outers (DESIGN.md §11.9 + §12.10).
	holeOwners := make([]int, len(holes))
	for hi, h := range holes {
		holeOwners[hi] = -1
		if len(h.poly) == 0 {
			continue
		}
		sample := h.poly[0]
		var ownerArea float64
		for i, o := range outers {
			if !o.bbox.Contains(sample) || !o.poly.Contains(sample) {
				continue
			}
			a := o.poly.Area()
			if holeOwners[hi] == -1 || a < ownerArea {
				holeOwners[hi] = i
				ownerArea = a
			}
		}
		if holeOwners[hi] < 0 {
			holes[hi].poly.Reverse()
			outers = append(outers, holes[hi])
			holes[hi] = classified{}
		}
	}

	// Nested-outer demotion: when the sweep emits both an outer ring and
	// an inner ring as CCW (e.g. Difference outer-minus-inner produces
	// both rings CCW because our FrontEdge convention doesn't naturally
	// reverse for holes), the inner-most must be demoted to a hole of
	// its enclosing outer. Detect by point-in-polygon containment.
	type outerOwner struct {
		idx  int // -1 if this outer is top-level; else index of containing outer
		area float64
	}
	owners := make([]outerOwner, len(outers))
	for i := range outers {
		owners[i] = outerOwner{idx: -1, area: outers[i].poly.Area()}
	}
	for i, oi := range outers {
		if len(oi.poly) == 0 {
			continue
		}
		// Sample with the centroid (average of vertices) — avoids
		// boundary-vertex false positives when two polygons touch at a
		// corner (sq1's vertex coincides with sq2's; that vertex would
		// be reported as inside sq2 by Polygon.Contains).
		sample := polyCentroid(oi.poly)
		for j, oj := range outers {
			if i == j || len(oj.poly) == 0 {
				continue
			}
			// Only the LARGER polygon can contain the smaller — protects
			// against mutual-containment false positives when both rings
			// share a centroid (concentric polygons).
			if owners[j].area <= owners[i].area {
				continue
			}
			if !oj.bbox.Contains(sample) || !oj.poly.Contains(sample) {
				continue
			}
			// oj contains oi. Track the SMALLEST containing outer.
			if owners[i].idx == -1 || owners[j].area < owners[owners[i].idx].area {
				owners[i].idx = j
			}
		}
	}

	// Determine nesting depth via parent chain. Even depth = outer; odd = hole.
	depth := func(i int) int {
		d := 0
		for owners[i].idx != -1 {
			i = owners[i].idx
			d++
		}
		return d
	}

	resultOuters := make([]int, 0, len(outers))
	for i := range outers {
		if depth(i)%2 == 0 {
			resultOuters = append(resultOuters, i)
		} else {
			// Demote to a hole of its parent outer. Reverse direction.
			outers[i].poly.Reverse()
		}
	}

	idxMap := make(map[int]int, len(resultOuters))
	result := make(MultiPolygon, len(resultOuters))
	for k, i := range resultOuters {
		idxMap[i] = k
		result[k] = ExPolygon{Outer: outers[i].poly}
	}
	// Attach demoted outers as holes of their parents.
	for i := range outers {
		if depth(i)%2 == 0 {
			continue
		}
		// Find the nearest outer ancestor (parent with even depth).
		ancestor := owners[i].idx
		for ancestor != -1 && depth(ancestor)%2 != 0 {
			ancestor = owners[ancestor].idx
		}
		if ancestor < 0 {
			continue
		}
		if k, ok := idxMap[ancestor]; ok {
			result[k].Holes = append(result[k].Holes, outers[i].poly)
		}
	}
	// Attach explicit CW holes.
	for hi, owner := range holeOwners {
		if owner < 0 || holes[hi].poly == nil {
			continue
		}
		if k, ok := idxMap[owner]; ok {
			result[k].Holes = append(result[k].Holes, holes[hi].poly)
		}
	}

	return result
}

// polyCentroid returns the average of the polygon's vertices — a point
// guaranteed strictly inside a convex polygon and almost always inside a
// well-formed concave polygon. Used as a containment-test sample point
// where polygon vertices themselves would give boundary false-positives.
func polyCentroid(p Polygon) Point {
	if len(p) == 0 {
		return Point{}
	}
	var sx, sy float64
	for _, v := range p {
		sx += v.X
		sy += v.Y
	}
	n := float64(len(p))
	return Point{X: sx / n, Y: sy / n}
}
