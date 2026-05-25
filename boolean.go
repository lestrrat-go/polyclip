package polyclip

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// ErrHorizontalNotSupported is returned by the boolean ops when the
// bound-model pre-pass [clip.BuildLocalMinima] fails (typically because
// shared vertices between rings broke topology reconstruction) AND the
// legacy per-edge fallback's [clip.ClassifyHorizontals] then encounters a
// mid-bound horizontal it can't classify. The bound model itself handles
// mid-bound horizontals natively (DESIGN.md §12.10), so well-formed input
// rings without shared vertices never hit this path.
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

	// Idempotency short-circuit: Union(A, A) = A. Identical inputs are a
	// degenerate case where every edge becomes a diff-src coincident pair
	// at the SAME vertex; the bound model's local-min disambiguation isn't
	// designed for that. Other diff-src coincident cases (overlapping but
	// not identical, e.g. TestUnionOverlappingAxisAligned) are resolved by
	// the sweep's winding classification over first-class horizontal AEL
	// edges (DESIGN.md §12.6.1).
	if mpolyEqual(a, b) {
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

// UnionAll returns the union of all inputs. It is functionally equivalent
// to repeated [Union], but pairs inputs in a tournament so the total work
// is O(n) Union calls of roughly balanced size rather than the O(n²)
// cumulative reduction `Union(Union(Union(p0, p1), p2), p3)…`.
//
// Empty input slice returns an empty [MultiPolygon]; a single-element
// slice returns that element unchanged.
func UnionAll(polys ...MultiPolygon) (MultiPolygon, error) {
	if len(polys) == 0 {
		return MultiPolygon{}, nil
	}
	if len(polys) == 1 {
		return polys[0], nil
	}
	// Work on a local copy so the caller's slice isn't mutated when we
	// overwrite entries between rounds.
	current := make([]MultiPolygon, len(polys))
	copy(current, polys)
	for len(current) > 1 {
		n := len(current)
		// Pair-merge in place: result of i and i+1 goes into slot i/2.
		// An unpaired trailing element survives to the next round.
		write := 0
		for i := 0; i+1 < n; i += 2 {
			merged, err := Union(current[i], current[i+1])
			if err != nil {
				return nil, err
			}
			current[write] = merged
			write++
		}
		if n%2 == 1 {
			current[write] = current[n-1]
			write++
		}
		current = current[:write]
	}
	return current[0], nil
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
	// Idempotency short-circuit: Intersect(A, A) = A. See [Union] note.
	if mpolyEqual(a, b) {
		return a, nil
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
	// Identity short-circuit: Difference(A, A) = ∅.
	if mpolyEqual(a, b) {
		return MultiPolygon{}, nil
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
	// Identity short-circuit: Xor(A, A) = ∅.
	if mpolyEqual(a, b) {
		return MultiPolygon{}, nil
	}
	if !a.BoundingBox().Intersects(b.BoundingBox()) {
		out := make(MultiPolygon, 0, len(a)+len(b))
		out = append(out, a...)
		out = append(out, b...)
		return out, nil
	}
	return runBooleanOp(a, b, clip.OpXor)
}

// Simplify resolves self-intersections and self-overlaps in m, returning an
// equivalent MultiPolygon whose rings are simple (non-self-intersecting) under
// the non-zero winding rule — the same convention the boolean ops use (CCW
// outer, CW hole). It runs the Vatti engine over m as a single source: a
// figure-eight splits into its two oppositely-wound loops, a ring traced twice
// collapses to one, and a doubled-back spur cancels.
//
// Simplify is the correct way to clean a self-intersecting polygon — for
// example the outward offset of a sharply concave ring. Running [Union] of m
// with itself does NOT work: identical inputs hit an idempotency
// short-circuit and are returned unchanged, leaving the self-intersection in
// place. [MultiPolygon.Clean] is purely geometric and also cannot resolve it.
//
// Empty input returns an empty MultiPolygon. Transversal (general-position)
// self-intersections resolve exactly. A snapped collinear degeneracy — e.g.
// parallel coincident walls landing on a single integer grid line — is subject
// to the same fixed-point limitation as the rest of the engine (DESIGN.md
// §7.2); callers needing robustness there should use [Offset], which adds its
// own multi-frame resolution.
func Simplify(m MultiPolygon) (MultiPolygon, error) {
	if len(m) == 0 {
		return MultiPolygon{}, nil
	}
	bbox := m.BoundingBox()
	scale := fixed.ScaleFromBBox(bbox.Min.X, bbox.Min.Y, bbox.Max.X, bbox.Max.Y)

	segs := collectSegments(m, clip.Subject, scale)
	segs = clip.SplitOverlaps(segs)
	segs = clip.SplitTJunctions(segs)
	segs = clip.DedupCoincidentEdges(segs)
	sw := clip.Sweep(segs, clip.OpUnion)
	if sw.Err != nil {
		if errors.Is(sw.Err, clip.ErrUnsupportedHorizontal) {
			return nil, fmt.Errorf("%w: %v", ErrHorizontalNotSupported, sw.Err)
		}
		return nil, sw.Err
	}
	return assembleResult(sw.Rings, scale), nil
}

// mpolyEqual reports whether two MultiPolygons are deeply equal — same
// piece count, same outer ring vertices in the same order, same holes.
// Used by the idempotency short-circuits in [Union] / [Intersect] /
// [Difference] / [Xor] to bypass the engine when inputs are identical.
// Non-identical diff-src coincident inputs are resolved by the sweep's
// winding classification over first-class horizontal AEL edges (DESIGN.md
// §12.6.1); identical inputs are a degenerate case where every edge collapses
// to a same-vertex local minimum, which the bound-model pre-pass can't
// disambiguate.
func mpolyEqual(a, b MultiPolygon) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !polyEqual(a[i].Outer, b[i].Outer) {
			return false
		}
		if len(a[i].Holes) != len(b[i].Holes) {
			return false
		}
		for j := range a[i].Holes {
			if !polyEqual(a[i].Holes[j], b[i].Holes[j]) {
				return false
			}
		}
	}
	return true
}

func polyEqual(a, b Polygon) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	segs = clip.SplitTJunctions(segs)
	segs = clip.DedupCoincidentEdges(segs)
	sw := clip.Sweep(segs, op)
	if sw.Err != nil {
		if errors.Is(sw.Err, clip.ErrUnsupportedHorizontal) {
			return nil, fmt.Errorf("%w: %v", ErrHorizontalNotSupported, sw.Err)
		}
		return nil, sw.Err
	}
	res := assembleResult(sw.Rings, scale)
	// Subset invariant: A∖B ⊆ A and A∩B ⊆ A∩B. The sweep can over-trace a
	// cross-source bound past where it exits the op-region (DESIGN.md §7.6),
	// emitting a spurious piece that lies OUTSIDE the required superset. Drop any
	// result piece whose interior point violates the op's subset invariant — a
	// correct piece always satisfies it, so this never drops valid output. (Only
	// whole entirely-spurious pieces are caught; a diagonal on an otherwise-valid
	// piece is not — those are handled in the sweep.)
	if op == clip.OpDifference || op == clip.OpIntersect {
		kept := res[:0]
		for _, ex := range res {
			pt, ok := interiorPoint(ex.Outer)
			// Only test hole-free pieces: interiorPoint(Outer) ignores holes and may
			// land inside one of ex's own holes, where a.Contains is false, wrongly
			// dropping a valid holed piece. Spurious over-traced fragments are simple
			// (hole-free), so this still catches them.
			if !ok || len(ex.Holes) > 0 {
				kept = append(kept, ex)
				continue
			}
			keep := a.Contains(pt) // Difference: ⊆ A
			if op == clip.OpIntersect {
				keep = a.Contains(pt) && b.Contains(pt)
			}
			if keep {
				kept = append(kept, ex)
			}
		}
		res = kept
	}
	return res, nil
}

// collectSegments converts every input edge into a fixed-point Segment and
// returns the slice. Horizontal segments are kept; the engine classifies
// them in a pre-pass.
func collectSegments(m MultiPolygon, src clip.Source, scale fixed.Scale) []clip.Segment {
	var out []clip.Segment
	for _, ex := range m {
		appendRing(&out, ex.Outer, src, scale, true)
		for _, h := range ex.Holes {
			appendRing(&out, h, src, scale, false)
		}
	}
	return out
}

// appendRing emits ring's edges as fixed-point segments. The sweep's winding
// model derives each edge's WindDx from its direction (clip.signedContribution),
// so it assumes a canonical input orientation: CCW outers, CW holes. Inputs
// with the opposite winding invert WindDx for that source and misclassify
// (e.g. a CW subject made Union/Xor under-count). Normalize here by walking the
// ring in reverse when its signed area disagrees with wantCCW, so callers may
// pass either orientation.
//
// Collinear-through vertices (a vertex exactly on the straight line between its
// neighbours) are removed before emitting segments. They are geometrically
// redundant, but the bound model treats the extra segment's shared endpoint as
// a turn/maximum of its bound, so a flat-topped ring with a collinear vertex on
// the top edge mis-builds its rings (the collinear-mid-vertex degeneracy,
// DESIGN.md §12.11). Removal happens here, on the INPUT ring, before
// [clip.SplitOverlaps] / [clip.SplitTJunctions] introduce their own (intended)
// collinear split vertices at crossings.
func appendRing(dst *[]clip.Segment, ring Polygon, src clip.Source, scale fixed.Scale, wantCCW bool) {
	n := len(ring)
	if n < 3 {
		return
	}
	reverse := (ring.SignedArea() < 0) == wantCCW
	pts := make([]fixed.Point, 0, n)
	for i := range n {
		k := i
		if reverse {
			k = n - 1 - i
		}
		p := scale.Snap(ring[k].X, ring[k].Y)
		if len(pts) > 0 && pts[len(pts)-1] == p {
			continue
		}
		pts = append(pts, p)
	}
	// Drop a wrap-around duplicate (first == last) so simplifyCollinearRing never
	// sees a zero-length neighbour, which Orient2D reads as collinear and would
	// wrongly delete a real corner of a ring with a repeated vertex.
	for len(pts) >= 2 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	pts = simplifyCollinearRing(pts)
	m := len(pts)
	if m < 3 {
		return
	}
	for i := range m {
		j := i + 1
		if j == m {
			j = 0
		}
		seg := clip.NewSegment(pts[i], pts[j], src)
		if seg.Degenerate() {
			continue
		}
		*dst = append(*dst, seg)
	}
}

// simplifyCollinearRing removes vertices that lie exactly on the straight line
// between their cyclic neighbours from a closed ring of grid points. For a
// simple (non-self-intersecting) polygon every collinear vertex is a redundant
// through-vertex, so removal is an exact geometric no-op. The pass repeats to
// collapse runs of three or more collinear vertices down to the two endpoints.
// Consecutive duplicate points (including the wrap) are assumed already removed
// by the caller.
func simplifyCollinearRing(pts []fixed.Point) []fixed.Point {
	for len(pts) >= 3 {
		n := len(pts)
		kept := make([]fixed.Point, 0, n)
		for i := range n {
			prev := pts[(i-1+n)%n]
			next := pts[(i+1)%n]
			if fixed.Orient2D(prev, pts[i], next) == 0 {
				continue
			}
			kept = append(kept, pts[i])
		}
		if len(kept) == n {
			return pts
		}
		pts = kept
	}
	return pts
}

// assembleResult converts the sweep's closed output rings into a user-space
// MultiPolygon, classifying each ring as outer or hole by its signed area
// and grouping holes into their containing outer.
func assembleResult(rings []*clip.OutRec, scale fixed.Scale) MultiPolygon {
	type classified struct {
		poly Polygon
		bbox BBox
		area float64 // absolute (unsigned) area
	}
	var rings2 []classified

	// ringInside reports whether inner is nested within outer. The sweep's
	// output rings have pairwise-disjoint interiors (they partition the plane
	// into in/out), so two rings are either disjoint or one strictly contains
	// the other — never partially overlapping. Under that invariant, inner is
	// nested in outer iff a point of inner's OPEN interior lies inside outer.
	//
	// The sample must be a genuine interior point of inner, not a vertex or the
	// vertex centroid. When two rings merely touch, their shared vertices — and,
	// for a collinear shared edge, even the vertex centroid — land ON the other
	// ring's boundary, which Polygon.Contains counts as inside, wrongly nesting
	// polygons that only touch (the shared-vertex bug, DESIGN.md §12.11).
	// Conversely a hole emitted by the sweep can have ALL its vertices on the
	// enclosing outer's boundary (e.g. the Xor overlap rectangle whose corners
	// sit on the union outline), so a vertex-based test gives the opposite false
	// negative. An interior point of inner avoids both: if inner is nested it is
	// strictly inside outer; if the rings only touch it is strictly outside.
	ringInside := func(inner, outer classified) bool {
		pt, ok := interiorPoint(inner.poly)
		if !ok {
			return false
		}
		return outer.bbox.Contains(pt) && outer.poly.Contains(pt)
	}

	for _, r := range rings {
		if r.Pts == nil {
			continue
		}
		base := dedupConsecutive(r.Points())
		if len(base) < 3 {
			continue
		}
		// A ring that revisits a vertex is self-touching — two boundary loops
		// meeting at a shared point, produced when an input vertex lies on the
		// other source's edge (vertex-on-edge degeneracy) or when the sweep
		// merges rings at a same-side maximum (AddLocalMaxPoly's figure-8 pinch).
		// Decompose into simple loops so each is classified independently; a
		// single self-touching cycle would yield a wrong net shoelace area.
		// Simple rings have no repeats and pass through unchanged.
		for _, fixedPts := range splitSelfTouchingRings(base) {
			if len(fixedPts) < 3 {
				continue
			}
			poly := make(Polygon, len(fixedPts))
			for i, fp := range fixedPts {
				poly[i].X, poly[i].Y = scale.Unsnap(fp)
			}
			rings2 = append(rings2, classified{
				poly: poly, bbox: poly.BoundingBox(),
				area: math.Abs(poly.SignedArea()),
			})
		}
	}

	// Containment forest over ALL rings, regardless of orientation. The sweep's
	// output rings have pairwise-disjoint interiors, so any two are either
	// disjoint or one strictly contains the other. parent[i] is the smallest
	// ring containing ring i (its immediate container), or -1 if i is
	// top-level. The sweep's own CCW/CW orientation is NOT used to classify
	// outer-vs-hole: it is locally meaningful but does not encode nesting depth
	// (an island inside a hole is CCW yet must become a filled top-level piece;
	// an Intersect cycle can emit a sole region CW). Depth parity is the only
	// reliable signal — see DESIGN.md §11.9 / §12.10.
	//
	// canContain reports whether j may be i's container. A strictly larger ring
	// can. Two coincident rings have EQUAL area and mutually contain each other
	// (same boundary; e.g. Difference/Xor of identical inputs emits the region
	// once CCW and once CW, which must cancel to zero area): the tie is broken
	// by orientation — the filled (CCW) ring is the parent, the hole (CW) ring
	// the child — so the pair nests as outer+hole and cancels.
	canContain := func(j, i int) bool {
		if rings2[j].area > rings2[i].area {
			return true
		}
		return rings2[j].area == rings2[i].area &&
			rings2[j].poly.SignedArea() > 0 && rings2[i].poly.SignedArea() < 0
	}
	parent := make([]int, len(rings2))
	for i := range rings2 {
		parent[i] = -1
		if len(rings2[i].poly) == 0 {
			continue
		}
		for j := range rings2 {
			if i == j || len(rings2[j].poly) == 0 {
				continue
			}
			if !canContain(j, i) {
				continue
			}
			if !ringInside(rings2[i], rings2[j]) {
				continue
			}
			// Prefer the smallest container; among equal-area coincident
			// containers any is fine (there is normally just one).
			if parent[i] == -1 || rings2[j].area < rings2[parent[i]].area {
				parent[i] = j
			}
		}
	}

	// depth(i) = number of rings strictly containing i. Even depth = a filled
	// region (the outer of an ExPolygon); odd depth = a hole. A hole's parent
	// has depth one less, so it is always an even-depth filled ring — attach
	// the hole directly to it. A filled ring at depth ≥ 2 (an island inside a
	// hole) is a top-level ExPolygon of its own (MultiPolygon is flat).
	depth := func(i int) int {
		d := 0
		for parent[i] != -1 {
			i = parent[i]
			d++
		}
		return d
	}

	idxMap := make(map[int]int, len(rings2))
	var result MultiPolygon
	for i := range rings2 {
		if len(rings2[i].poly) == 0 || depth(i)%2 != 0 {
			continue
		}
		if rings2[i].poly.SignedArea() < 0 { // a filled ring must be CCW
			rings2[i].poly.Reverse()
		}
		idxMap[i] = len(result)
		result = append(result, ExPolygon{Outer: rings2[i].poly})
	}
	for i := range rings2 {
		if len(rings2[i].poly) == 0 || depth(i)%2 == 0 {
			continue
		}
		if rings2[i].poly.SignedArea() > 0 { // a hole must be CW
			rings2[i].poly.Reverse()
		}
		if k, ok := idxMap[parent[i]]; ok {
			result[k].Holes = append(result[k].Holes, rings2[i].poly)
		}
	}

	return result
}

// dedupConsecutive removes consecutive identical points from a closed ring,
// including the wrap-around (last == first). The sweep can emit a vertex
// twice at a maxima confluence — once when a hot maxima edge is crossed past
// a cold co-maximum edge (the one-hot IntersectEdges branch adds the apex on
// one ring side) and again when AddLocalMaxPoly closes the ring on the other
// side — leaving a zero-length edge. Clipper2 strips these in its output
// stage (BuildPath); this is the equivalent cleanup.
// splitSelfTouchingRings decomposes a closed ring whose vertex list repeats a
// vertex into simple loops. Walking the vertices, whenever the walk returns to
// a vertex already on the open path, the run since that vertex forms a closed
// sub-loop and is split off; the shared vertex stays on the continuing path.
// The result loops are each simple (no internal repeats). A ring with no
// repeated vertex returns as a single loop unchanged.
func splitSelfTouchingRings(pts []fixed.Point) [][]fixed.Point {
	var loops [][]fixed.Point
	stack := make([]fixed.Point, 0, len(pts))
	at := make(map[fixed.Point]int, len(pts))
	for _, p := range pts {
		if j, ok := at[p]; ok {
			loop := make([]fixed.Point, len(stack)-j)
			copy(loop, stack[j:])
			loops = append(loops, loop)
			for k := j; k < len(stack); k++ {
				delete(at, stack[k])
			}
			stack = stack[:j]
		}
		stack = append(stack, p)
		at[p] = len(stack) - 1
	}
	if len(stack) > 0 {
		loops = append(loops, stack)
	}
	return loops
}

func dedupConsecutive(pts []fixed.Point) []fixed.Point {
	if len(pts) < 2 {
		return pts
	}
	out := make([]fixed.Point, 0, len(pts))
	out = append(out, pts[0])
	for _, p := range pts[1:] {
		if p == out[len(out)-1] {
			continue
		}
		out = append(out, p)
	}
	for len(out) >= 2 && out[len(out)-1] == out[0] {
		out = out[:len(out)-1]
	}
	return out
}

// interiorPoint returns a point strictly inside the simple polygon p, and a
// bool reporting success. It casts a horizontal ray through the polygon's
// vertex-Y centroid, collects the edge crossings, and returns the midpoint of
// the widest interior span. That point is guaranteed strictly inside p (it sits
// between an entering and leaving crossing of a well-formed ring), independent
// of whether p is convex — unlike the vertex centroid, which can fall outside a
// concave ring or land on a neighbour's boundary. Used by nesting detection so
// that two rings which merely touch are never reported as nested (DESIGN.md
// §12.11). Returns false for degenerate rings (<3 vertices, or no interior span
// found, e.g. zero area).
func interiorPoint(p Polygon) (Point, bool) {
	n := len(p)
	if n < 3 {
		return Point{}, false
	}
	// Choose the scanline Y strictly between two adjacent distinct vertex Ys
	// (the widest gap), so it grazes no vertex and runs along no horizontal
	// edge. A Y equal to a vertex — e.g. the mean coinciding with a horizontal
	// edge the ring shares with another — makes the "interior" span run along
	// the ring's own boundary, returning a boundary point that Polygon.Contains
	// treats as inside the other ring and wrongly nests touching polygons
	// (DESIGN.md §12.11).
	ys := make([]float64, n)
	for i, v := range p {
		ys[i] = v.Y
	}
	sort.Float64s(ys)
	gapLo, gap := 0.0, 0.0
	for i := 0; i+1 < n; i++ {
		if g := ys[i+1] - ys[i]; g > gap {
			gap, gapLo = g, ys[i]
		}
	}
	if gap <= 0 {
		return Point{}, false // degenerate: all vertices share one Y
	}
	y := gapLo + gap/2

	var xs []float64
	for i := range n {
		a := p[i]
		b := p[(i+1)%n]
		// Half-open [min.Y, max.Y) crossing test: counts each edge once and
		// avoids double-counting at shared vertices.
		if (a.Y <= y) == (b.Y <= y) {
			continue
		}
		t := (y - a.Y) / (b.Y - a.Y)
		xs = append(xs, a.X+t*(b.X-a.X))
	}
	if len(xs) < 2 {
		return Point{}, false
	}
	sort.Float64s(xs)

	// Interior spans are the (0,1),(2,3),… pairs. Pick the widest so the
	// midpoint sits well clear of both boundaries.
	bestLo, bestHi, bestW := 0.0, 0.0, -1.0
	for i := 0; i+1 < len(xs); i += 2 {
		if w := xs[i+1] - xs[i]; w > bestW {
			bestW, bestLo, bestHi = w, xs[i], xs[i+1]
		}
	}
	if bestW <= 0 {
		return Point{}, false
	}
	return Point{X: (bestLo + bestHi) / 2, Y: y}, true
}
