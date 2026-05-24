package clip

import (
	"sort"

	"github.com/lestrrat-go/polyclip/fixed"
)

// SplitOverlaps takes a list of segments and returns a new list where every
// pair of partially-overlapping collinear segments has been split at the
// overlap endpoints. After this transformation the only collinear pairs that
// remain are either disjoint or fully coincident (identical Bot and Top) —
// the sweep relies on this invariant.
//
// Degenerate (zero-length) segments are dropped.
//
// Two segments can only overlap if they share a supporting line, so we bucket
// the survivors by their exact (integer) supporting line and resolve each
// bucket independently. Within a bucket every endpoint is a potential split
// point: a segment is cut at each other segment's endpoint that lies strictly
// in its interior. After the cuts no segment's interior contains another's
// endpoint, so any remaining collinear pair is disjoint or fully coincident —
// the required invariant. Segments are emitted expanded in place, preserving
// input order (a bucket of one passes through unchanged).
//
// Complexity is O(n) to bucket plus, per line bucket of m segments, O(m²) to
// test endpoint containment — versus the previous global O(n³) pairwise scan.
// For the common case of few collinear segments per line this is effectively
// linear.
func SplitOverlaps(segs []Segment) []Segment {
	byLine := make(map[lineKey][]int, len(segs))
	for i := range segs {
		if segs[i].Degenerate() {
			continue
		}
		k := lineOf(segs[i])
		byLine[k] = append(byLine[k], i)
	}

	out := make([]Segment, 0, len(segs))
	for i := range segs {
		if segs[i].Degenerate() {
			continue
		}
		group := byLine[lineOf(segs[i])]
		if len(group) == 1 {
			out = append(out, segs[i])
			continue
		}
		out = append(out, splitAtInteriorEndpoints(segs[i], group, segs)...)
	}
	return out
}

// splitAtInteriorEndpoints cuts s at every endpoint of the other segments in
// its collinear group that lies strictly inside s, returning the ordered
// pieces. group holds indices into segs; all are collinear with s.
func splitAtInteriorEndpoints(s Segment, group []int, segs []Segment) []Segment {
	var cuts []fixed.Point
	seen := map[fixed.Point]struct{}{}
	consider := func(p fixed.Point) {
		if !LessYX(s.Bot, p) || !LessYX(p, s.Top) {
			return // not strictly interior to s
		}
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		cuts = append(cuts, p)
	}
	for _, k := range group {
		consider(segs[k].Bot)
		consider(segs[k].Top)
	}
	if len(cuts) == 0 {
		return []Segment{s}
	}
	sortPointsYX(cuts)

	pieces := make([]Segment, 0, len(cuts)+1)
	cur := s.Bot
	for _, p := range cuts {
		pieces = append(pieces, makeSegment(cur, p, s.Src, s.Reversed))
		cur = p
	}
	pieces = append(pieces, makeSegment(cur, s.Top, s.Src, s.Reversed))
	return pieces
}

// sortPointsYX sorts points ascending in (Y, X) order.
func sortPointsYX(pts []fixed.Point) {
	sort.Slice(pts, func(i, j int) bool { return LessYX(pts[i], pts[j]) })
}

// lineKey identifies a segment's supporting line exactly: a gcd-reduced
// direction plus the signed line offset b·X − a·Y, which is constant along the
// line and distinct for parallel lines. The offset is held in 128 bits so it
// is exact for the full [fixed.MaxCoordMagnitude] grid (the products overflow
// int64).
type lineKey struct {
	a, b int64
	off  fixed.I128
}

// lineOf returns the supporting line of s. Two segments are collinear iff their
// lineKeys are equal.
func lineOf(s Segment) lineKey {
	d := direction(s)
	a, b := d[0], d[1]
	off := fixed.MulI64(b, int64(s.Bot.X)).Sub(fixed.MulI64(a, int64(s.Bot.Y)))
	return lineKey{a: a, b: b, off: off}
}

// direction returns s's gcd-reduced direction vector. Segments are stored
// canonically (Bot < Top in (Y, X) order), so dy > 0, or dy == 0 with dx > 0;
// the reduced vector is therefore already sign-canonical and two collinear
// segments always reduce to the identical key. The computation uses only
// coordinate differences and a gcd, never a product, so it cannot overflow.
func direction(s Segment) [2]int64 {
	dx := int64(s.Top.X - s.Bot.X)
	dy := int64(s.Top.Y - s.Bot.Y)
	g := gcd64(dx, dy)
	if g == 0 {
		return [2]int64{dx, dy} // degenerate; callers drop these beforehand
	}
	return [2]int64{dx / g, dy / g}
}

// gcd64 returns the non-negative greatest common divisor of |a| and |b|.
func gcd64(a, b int64) int64 {
	if a < 0 {
		a = -a
	}
	if b < 0 {
		b = -b
	}
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// splitAt returns the pieces of s after splitting at the two interior or
// endpoint points p and q. The pieces preserve s's source and Reversed flag.
// p and q must both lie on s; the function does not verify this.
//
// If p or q coincides with s.Bot or s.Top, the corresponding boundary piece
// is omitted. Degenerate (zero-length) pieces are dropped.
func splitAt(s Segment, p, q fixed.Point) []Segment {
	if LessYX(q, p) {
		p, q = q, p
	}
	out := make([]Segment, 0, 3)
	if p != s.Bot {
		out = append(out, makeSegment(s.Bot, p, s.Src, s.Reversed))
	}
	if p != q {
		out = append(out, makeSegment(p, q, s.Src, s.Reversed))
	}
	if q != s.Top {
		out = append(out, makeSegment(q, s.Top, s.Src, s.Reversed))
	}
	return out
}

// makeSegment constructs a Segment with explicit fields, bypassing the
// canonicalisation done by [NewSegment]. It is used by [splitAt] which has
// already verified that bot < top in LessYX order.
func makeSegment(bot, top fixed.Point, src Source, reversed bool) Segment {
	return Segment{Bot: bot, Top: top, Src: src, Reversed: reversed}
}

// SplitTJunctions splits any segment whose interior is touched by an endpoint
// (vertex) of another segment, inserting that vertex as a shared endpoint. A
// "T-junction" is a vertex of one edge lying strictly inside another edge (not
// at its endpoints). This establishes the invariant "no vertex lies in the
// open interior of any edge" — the sibling of [SplitOverlaps]'s "no partial
// collinear overlaps".
//
// The split point is the touching vertex itself — an existing grid coordinate
// — so no new rounding is introduced and the transform is area-preserving.
//
// This is a PRECONDITION for shared-vertex crossing dispatch, not a fix on its
// own: it converts a vertex-on-edge into a coincident shared vertex, which the
// sweep currently still mishandles (the crossing of two bounds that swap order
// exactly at the shared vertex is never dispatched). See DESIGN.md §12.11,
// vertex-on-edge track.
//
// Run AFTER [SplitOverlaps] (so collinear overlaps are already resolved to
// disjoint or fully-coincident) and before [DedupCoincidentEdges].
//
// Splitting a segment at a T-junction inserts an existing vertex as a shared
// endpoint, so it creates no new vertex. The set of vertices is therefore
// fixed, and a single batch pass — cut every segment at every vertex strictly
// in its interior — establishes the invariant with no fixpoint. Candidate
// vertices are found through an X-sorted index of the distinct vertices: for a
// segment we binary-search its X-extent and test only that window, instead of
// scanning every other segment. Complexity is O(n log n) plus the cost of the
// candidates actually inside each segment's bounding box, versus the previous
// global O(n³) pairwise scan.
func SplitTJunctions(segs []Segment) []Segment {
	verts := distinctVerticesByX(segs)

	out := make([]Segment, 0, len(segs))
	for i := range segs {
		s := segs[i]
		if s.Degenerate() {
			continue
		}
		cuts := interiorVertices(s, verts)
		if len(cuts) == 0 {
			out = append(out, s)
			continue
		}
		sortPointsYX(cuts)
		cur := s.Bot
		for _, p := range cuts {
			out = append(out, makeSegment(cur, p, s.Src, s.Reversed))
			cur = p
		}
		out = append(out, makeSegment(cur, s.Top, s.Src, s.Reversed))
	}
	return out
}

// distinctVerticesByX returns the distinct endpoints of the non-degenerate
// segments, sorted ascending by (X, Y) for binary-search lookup.
func distinctVerticesByX(segs []Segment) []fixed.Point {
	seen := make(map[fixed.Point]struct{}, 2*len(segs))
	verts := make([]fixed.Point, 0, 2*len(segs))
	add := func(p fixed.Point) {
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		verts = append(verts, p)
	}
	for i := range segs {
		if segs[i].Degenerate() {
			continue
		}
		add(segs[i].Bot)
		add(segs[i].Top)
	}
	sort.Slice(verts, func(i, j int) bool {
		if verts[i].X != verts[j].X {
			return verts[i].X < verts[j].X
		}
		return verts[i].Y < verts[j].Y
	})
	return verts
}

// interiorVertices returns the vertices in vertsByX (sorted by X, Y) that lie
// strictly inside segment s — collinear with s, within its bounding box, and
// not equal to either endpoint. These are exactly the T-junction split points
// for s. The X-extent is located by binary search so only candidates in s's
// X-range are tested; the on-segment test uses 128-bit [fixed.Orient2D].
func interiorVertices(s Segment, vertsByX []fixed.Point) []fixed.Point {
	minX, maxX := s.Bot.X, s.Top.X
	if minX > maxX {
		minX, maxX = maxX, minX
	}
	minY, maxY := s.Bot.Y, s.Top.Y // canonical: Bot.Y <= Top.Y
	lo := sort.Search(len(vertsByX), func(i int) bool { return vertsByX[i].X >= minX })

	var cuts []fixed.Point
	for i := lo; i < len(vertsByX) && vertsByX[i].X <= maxX; i++ {
		v := vertsByX[i]
		if v == s.Bot || v == s.Top {
			continue
		}
		if v.Y < minY || v.Y > maxY {
			continue
		}
		if fixed.Orient2D(s.Bot, s.Top, v) != 0 {
			continue // in the bounding box but off the supporting line
		}
		cuts = append(cuts, v)
	}
	return cuts
}

// DedupCoincidentEdges handles the same-source §11.7 cases:
//
//   - Same source, same direction (duplicate input edge): keep one, drop
//     the rest.
//   - Same source, opposite direction: cancel — drop both.
//
// These transformations preserve ring topology (they only remove edges
// that were already redundant or cancelling). Different-source coincident
// pairs are left for the sweep: with horizontals as first-class AEL edges
// (DESIGN.md §12.6.1), a coincident pair is resolved by the standard winding
// classification as the edges are processed, no special handling required.
//
// Complexity O(n) per coincident group, O(n²) worst case via grouping.
func DedupCoincidentEdges(segs []Segment) []Segment {
	type key struct{ bot, top fixed.Point }
	groups := make(map[key][]int, len(segs))
	for i := range segs {
		s := &segs[i]
		if s.Degenerate() {
			continue
		}
		k := key{s.Bot, s.Top}
		groups[k] = append(groups[k], i)
	}

	dropped := make(map[int]struct{})
	for _, idxs := range groups {
		if len(idxs) < 2 {
			continue
		}
		applySameSrcRules(segs, idxs, dropped)
	}

	if len(dropped) == 0 {
		return segs
	}
	out := make([]Segment, 0, len(segs)-len(dropped))
	for i, s := range segs {
		if _, drop := dropped[i]; drop {
			continue
		}
		if s.Degenerate() {
			continue
		}
		out = append(out, s)
	}
	return out
}

// applySameSrcRules processes one group of fully-coincident segments per
// §11.7's same-source cases. Different-source cases are left for the sweep,
// where first-class horizontal AEL edges resolve them via winding
// classification (DESIGN.md §12.6.1).
func applySameSrcRules(segs []Segment, idxs []int, dropped map[int]struct{}) {
	// Partition by (Src, Reversed).
	var subjFwd, subjRev, clipFwd, clipRev []int
	for _, i := range idxs {
		s := &segs[i]
		switch {
		case s.Src == Subject && !s.Reversed:
			subjFwd = append(subjFwd, i)
		case s.Src == Subject && s.Reversed:
			subjRev = append(subjRev, i)
		case s.Src == Clip && !s.Reversed:
			clipFwd = append(clipFwd, i)
		case s.Src == Clip && s.Reversed:
			clipRev = append(clipRev, i)
		}
	}

	// Same-source same-direction duplicates: keep first, drop the rest.
	dedupExtras := func(group []int) {
		for _, i := range group[1:] {
			dropped[i] = struct{}{}
		}
	}
	if len(subjFwd) > 1 {
		dedupExtras(subjFwd)
		subjFwd = subjFwd[:1]
	}
	if len(subjRev) > 1 {
		dedupExtras(subjRev)
		subjRev = subjRev[:1]
	}
	if len(clipFwd) > 1 {
		dedupExtras(clipFwd)
		clipFwd = clipFwd[:1]
	}
	if len(clipRev) > 1 {
		dedupExtras(clipRev)
		clipRev = clipRev[:1]
	}

	// Same-source opposite-direction pairs cancel. We only need to know
	// how many pairs to drop, then take that many indices from each side.
	pairCancel := func(fwd, rev []int) {
		n := min(len(fwd), len(rev))
		for i := range n {
			dropped[fwd[i]] = struct{}{}
			dropped[rev[i]] = struct{}{}
		}
	}
	pairCancel(subjFwd, subjRev)
	pairCancel(clipFwd, clipRev)
}
