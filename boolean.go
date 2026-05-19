package polyclip

import (
	"errors"
	"fmt"

	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// ErrHorizontalNotSupported is returned by [Union] (and future boolean ops)
// when an input contains an axis-aligned horizontal edge. The current engine
// processes only non-horizontal segments; horizontal handling is the subject
// of a future increment (see DESIGN.md §12.6).
var ErrHorizontalNotSupported = errors.New("polyclip: input contains a horizontal edge — not yet supported by the Vatti engine")

// Union returns a ∪ b.
//
// Handled cases:
//
//   - Empty inputs: Union(empty, b) returns b unchanged, Union(a, empty)
//     returns a.
//   - Strictly disjoint bounding boxes: equivalent to concatenation. The
//     two MultiPolygons are returned spliced together with no engine work.
//   - Overlapping or boundary-touching inputs with no horizontal edges:
//     the Vatti engine in [github.com/lestrrat-go/polyclip/clip] runs over
//     the snapped segments. Output rings are converted back to a float64
//     MultiPolygon. Hole assignment uses signed-area sign and bbox-prefilter
//     point-in-polygon (DESIGN.md §11.9).
//
// Inputs containing horizontal edges return [ErrHorizontalNotSupported].
// This is a known limitation that will be lifted in a future increment.
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

// runBooleanOp is the engine path: snap inputs to a fixed-point grid, feed
// segments through the sweep, and convert rings back to a user-space
// MultiPolygon.
func runBooleanOp(a, b MultiPolygon, op clip.Operation) (MultiPolygon, error) {
	bbox := a.BoundingBox().Union(b.BoundingBox())
	scale := fixed.ScaleFromBBox(bbox.Min.X, bbox.Min.Y, bbox.Max.X, bbox.Max.Y)

	segs, err := collectSegments(a, clip.Subject, scale)
	if err != nil {
		return nil, err
	}
	bSegs, err := collectSegments(b, clip.Clip, scale)
	if err != nil {
		return nil, err
	}
	segs = append(segs, bSegs...)

	segs = clip.SplitOverlaps(segs)
	sw := clip.Sweep(segs, op)
	return assembleResult(sw.Rings, scale), nil
}

// collectSegments converts every input edge into a fixed-point Segment and
// returns the slice. Horizontal segments cause an immediate error.
func collectSegments(m MultiPolygon, src clip.Source, scale fixed.Scale) ([]clip.Segment, error) {
	var out []clip.Segment
	for _, ex := range m {
		if err := appendRing(&out, ex.Outer, src, scale); err != nil {
			return nil, err
		}
		for _, h := range ex.Holes {
			if err := appendRing(&out, h, src, scale); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func appendRing(dst *[]clip.Segment, ring Polygon, src clip.Source, scale fixed.Scale) error {
	n := len(ring)
	if n < 3 {
		return nil
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
		if seg.Horizontal() {
			return fmt.Errorf("%w: ring vertex %v→%v lies on the same Y", ErrHorizontalNotSupported, ring[i], ring[j])
		}
		*dst = append(*dst, seg)
	}
	return nil
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

	result := make(MultiPolygon, len(outers))
	for i, o := range outers {
		result[i] = ExPolygon{Outer: o.poly}
	}

	// Assign each hole to the outer with the smallest bbox that contains a
	// sample vertex of the hole (DESIGN.md §11.9).
	for _, h := range holes {
		if len(h.poly) == 0 {
			continue
		}
		sample := h.poly[0]
		owner := -1
		var ownerArea float64
		for i, o := range outers {
			if !o.bbox.Contains(sample) {
				continue
			}
			if !o.poly.Contains(sample) {
				continue
			}
			a := o.poly.Area()
			if owner == -1 || a < ownerArea {
				owner = i
				ownerArea = a
			}
		}
		if owner >= 0 {
			result[owner].Holes = append(result[owner].Holes, h.poly)
		}
	}

	return result
}
