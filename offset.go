package polyclip

import (
	"errors"
	"math"

	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// JoinType selects the geometry used at convex corners of an offset.
type JoinType int

const (
	// JoinMiter extends the two adjacent offset edges to their intersection
	// (a sharp corner). When the resulting miter length exceeds
	// [OffsetOptions.MiterLimit] · |d| the join is bevelled to a chamfer.
	JoinMiter JoinType = iota
	// JoinRound replaces the corner with a circular arc tessellated to
	// segments with chord deviation ≤ [OffsetOptions.ArcTol].
	JoinRound
	// JoinSquare replaces the corner with a square (45° chamfer) regardless
	// of the corner's actual angle.
	JoinSquare
)

// EndType is reserved for future open-path offset support; closed
// polygons (the only currently-supported input) always behave as
// [EndPolygon].
type EndType int

const (
	// EndPolygon offsets a closed polygonal region — the Minkowski sum of
	// the input with a disk for positive d, the erosion for negative d.
	EndPolygon EndType = iota
)

// OffsetOptions configures [Offset]. Zero values pick documented defaults.
type OffsetOptions struct {
	Join       JoinType // default JoinMiter
	MiterLimit float64  // multiplier on |d| beyond which miters are bevelled. Default 2.0.
	ArcTol     float64  // max chord deviation for round joins, in user units. Default abs(d) * 0.01.
}

// ErrOffsetEmpty is returned by [Offset] when the input is empty or
// every ring vanishes under the requested offset (e.g. inward offset
// larger than the smallest feature).
var ErrOffsetEmpty = errors.New("polyclip: offset produced empty result")

// Offset returns the Minkowski sum of m with a disk of radius d when
// d > 0 (outward offset / inflation), or the Minkowski erosion when
// d < 0 (inward offset / deflation). Per DESIGN.md §4.3 the algorithm
// walks each input ring and emits an offset ring directly — at each
// vertex it places either the miter apex (when adjacent offset edges
// cross on the offset side, e.g. inward offset of a convex corner) or
// a join (miter/round/square wedge filler, when offset edges leave a
// gap on the offset side, e.g. outward offset of a convex corner).
//
// Topology changes are handled (DESIGN.md §7.1): when an inward offset
// pinches a ring in two (a dumbbell past its neck) or closes a notch, or
// when an outward offset of a concave ring self-intersects, the raw offset
// ring is re-resolved by a positive-fill self-union that splits or merges it
// into the correct simple pieces. A piece is emitted only where the offset
// region is non-empty; an over-shrunk piece collapses and is dropped, and if
// everything collapses Offset returns [ErrOffsetEmpty].
//
// Hole orientation: outer rings are CCW, holes are CW (the standard
// polyclip convention). A positive d inflates outer rings and shrinks
// holes; a negative d does the opposite. A hole that closes up under
// inward offset is absorbed; an outer ring that vanishes drops its piece.
//
// Use [OffsetOptions.Join] to pick the convex-corner geometry; see
// [JoinType] for choices. Default join is [JoinMiter] with miter
// limit 2.0.
func Offset(m MultiPolygon, d float64, opts OffsetOptions) (MultiPolygon, error) {
	if len(m) == 0 {
		return nil, ErrOffsetEmpty
	}
	if d == 0 {
		return cloneMulti(m), nil
	}
	if opts.MiterLimit <= 0 {
		opts.MiterLimit = 2.0
	}
	if opts.ArcTol <= 0 {
		opts.ArcTol = math.Abs(d) * 0.01
	}

	result := MultiPolygon{}
	for _, ex := range m {
		// Build the raw offset rings for this piece: the outer offset by d,
		// each hole by -d. Right-hand normal of a CCW ring points outward, so
		// positive d grows the outer outward; a CW hole offset by -d grows the
		// printable region by shrinking the hole.
		//
		// The rings are NOT validated or rejected individually here — an
		// inward offset that overshoots produces a self-intersecting ring, and
		// resolveOffsetPiece re-resolves the topology (splitting a pinched ring
		// into islands, dropping inside-out collapses) via a positive-fill
		// self-union (DESIGN.md §7.1).
		rings := make([]Polygon, 0, 1+len(ex.Holes))
		if outer := offsetRing(ex.Outer, d, opts); len(outer) >= 3 {
			rings = append(rings, outer)
		}
		if len(rings) == 0 {
			continue
		}
		for _, h := range ex.Holes {
			if oh := offsetRing(h, -d, opts); len(oh) >= 3 {
				rings = append(rings, oh)
			}
		}
		for _, piece := range resolveOffsetPiece(rings) {
			// For inward offset, drop any piece that is not genuinely ≥|d|
			// inside the original — this catches the convex "inside-out"
			// collapse, whose offset ring is simple and positively oriented
			// yet sits where the offset region is empty (the erosion
			// definition: a result point must be ≥|d| from the input boundary).
			if d < 0 && !insetDeepEnough(piece, ex, math.Abs(d), opts.ArcTol) {
				continue
			}
			result = append(result, piece)
		}
	}
	if len(result) == 0 {
		return nil, ErrOffsetEmpty
	}
	return result, nil
}

// insetDeepEnough reports whether piece lies at least dist inside the original
// ExPolygon orig — i.e. an interior point of piece is ≥ dist from every edge
// of orig (its outer and holes). This is the erosion criterion used to reject
// the inside-out collapse of an over-shrunk convex ring. The tolerance absorbs
// round-join chord deviation (chords sit up to arcTol inside the true arc).
func insetDeepEnough(piece ExPolygon, orig ExPolygon, dist, arcTol float64) bool {
	pt, ok := interiorPoint(piece.Outer)
	if !ok {
		return false
	}
	tol := arcTol + dist*1e-6
	minDist := math.Inf(1)
	scan := func(ring Polygon) {
		n := len(ring)
		for i := range n {
			if e := pointSegDist(pt, ring[i], ring[(i+1)%n]); e < minDist {
				minDist = e
			}
		}
	}
	scan(orig.Outer)
	for _, h := range orig.Holes {
		scan(h)
	}
	return minDist >= dist-tol
}

// pointSegDist returns the Euclidean distance from p to segment ab.
func pointSegDist(p, a, b Point) float64 {
	dx, dy := b.X-a.X, b.Y-a.Y
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(p.X-a.X, p.Y-a.Y)
	}
	t := ((p.X-a.X)*dx + (p.Y-a.Y)*dy) / l2
	t = max(0, min(1, t))
	return math.Hypot(p.X-(a.X+t*dx), p.Y-(a.Y+t*dy))
}

// resolveOffsetPiece turns the raw offset rings of one input ExPolygon (the
// offset outer first, then the offset holes) into clean, simple output
// pieces. The outer ring is CCW (positive winding inside) and the hole rings
// are CW (negative winding inside), so the printable region is exactly where
// the combined winding is strictly positive.
//
// When none of the rings self-intersect or cross each other the offset did
// not change topology: the rings are returned directly as one ExPolygon
// (exact, no engine pass), with the outer dropped if it inverted (an inward
// offset past the inradius). Otherwise the rings are re-resolved by a
// positive-fill self-union (DESIGN.md §7.1): the sweep splits a pinched ring
// into islands and drops the negatively-wound overshoot folds.
func resolveOffsetPiece(rings []Polygon) MultiPolygon {
	if len(rings) == 0 {
		return nil
	}
	if !ringsIntersect(rings) {
		// Topology unchanged. The outer (rings[0]) is valid only if it kept
		// CCW orientation; an inverted outer collapsed to nothing.
		if rings[0].SignedArea() <= 0 {
			return nil
		}
		piece := ExPolygon{Outer: rings[0]}
		for _, h := range rings[1:] {
			if h.SignedArea() < 0 { // a real hole stayed CW
				piece.Holes = append(piece.Holes, h)
			}
		}
		return MultiPolygon{piece}
	}
	return selfUnionPositive(rings)
}

// offsetRing walks ring once and emits a new polygon offset by d. With
// n_i = right-hand unit normal of edge ring[i]→ring[i+1], each input
// vertex v = ring[i] expands into one or more output vertices:
//
//   - If the two adjacent offset edges leave a wedge gap on the offset
//     side (convex corner for outward d>0, reflex corner for inward
//     d<0), emit a join: a single miter apex, a chamfer pair, or an
//     arc-tessellated sequence, per [OffsetOptions.Join].
//   - If the two adjacent offset edges cross on the offset side (reflex
//     corner for outward d>0, convex corner for inward d<0), emit just
//     the miter apex (the intersection point).
//   - Collinear: emit a single offset point.
//
// Degenerate edges (zero-length) are skipped; if two consecutive
// vertices coincide, the prior edge's normal is reused.
func offsetRing(ring Polygon, d float64, opts OffsetOptions) Polygon {
	n := len(ring)
	if n < 3 {
		return nil
	}
	// Right-hand unit normals per edge ring[i]→ring[(i+1)%n].
	normals := make([]Point, n)
	have := make([]bool, n)
	for i := range n {
		a, b := ring[i], ring[(i+1)%n]
		dx, dy := b.X-a.X, b.Y-a.Y
		l := math.Hypot(dx, dy)
		if l == 0 {
			continue
		}
		normals[i] = Point{X: dy / l, Y: -dx / l}
		have[i] = true
	}
	// Carry the most recent valid normal forward and backward to fill any
	// degenerate-edge gaps.
	last := Point{}
	for i := range n {
		if have[i] {
			last = normals[i]
			break
		}
	}
	for i := range n {
		if !have[i] {
			normals[i] = last
		} else {
			last = normals[i]
		}
	}

	out := make(Polygon, 0, n+4)
	for i := range n {
		v := ring[i]
		prevN := normals[(i-1+n)%n]
		nextN := normals[i]
		emitVertex(&out, v, prevN, nextN, d, opts)
	}
	// The raw ring is returned as-is, even when an inward offset overshoots
	// the inradius and the ring self-intersects (a pinched neck, a collapsed
	// notch, an inside-out collapse). Topology is re-resolved by the
	// self-union in [resolveOffsetPiece] (DESIGN.md §7.1) rather than by a
	// whole-ring accept/reject, which would drop both islands of a dumbbell
	// that splits in two.
	return out
}

// emitVertex appends the offset-ring points for input vertex v with
// adjacent right-hand unit normals prevN (of edge prev→v) and nextN (of
// edge v→next). See offsetRing for the full classification.
func emitVertex(out *Polygon, v, prevN, nextN Point, d float64, opts OffsetOptions) {
	a := Point{X: v.X + d*prevN.X, Y: v.Y + d*prevN.Y} // end of prev offset at v
	c := Point{X: v.X + d*nextN.X, Y: v.Y + d*nextN.Y} // start of next offset at v
	// cross of the two normals (pn × nn). For CCW input rings with d>0,
	// positive cross = left turn = convex corner (offset wedge on the
	// outside). Sign-flipped by d for the unified rule.
	cross := prevN.X*nextN.Y - prevN.Y*nextN.X
	// Sign of cross*d: positive means offset side is a WEDGE that needs
	// filling with a join; non-positive means the offset edges cross on
	// the offset side and emitting the miter apex (or two perpendicular
	// points at sharp reflex angles) is enough.
	wedge := cross*d > 0
	if !wedge {
		// Concave-offset case — emit the miter apex (single point).
		appendMiterApex(out, v, prevN, nextN, d, true)
		return
	}
	// Convex-offset case — emit a join shape.
	switch opts.Join {
	case JoinRound:
		appendRoundJoin(out, v, a, c, d, opts.ArcTol)
	case JoinSquare:
		appendSquareJoin(out, v, a, c, prevN, nextN, d)
	default: // JoinMiter
		appendMiter(out, v, a, c, prevN, nextN, d, opts.MiterLimit)
	}
}

// appendMiterApex appends the intersection point of the two offset
// edges at vertex v. The two unit normals' bisector — scaled by
// d/(1 + n_prev·n_next) — gives the apex offset from v. If the normals
// are anti-parallel (no intersection), fall back to a chamfer (a, c) if
// allowed, or emit a single midpoint.
//
// When emitAlways is true (concave-offset / reflex case), always emit
// something — either the apex or, when the apex is degenerate, the two
// perpendicular endpoints a and c.
func appendMiterApex(out *Polygon, v, prevN, nextN Point, d float64, emitAlways bool) {
	bx := prevN.X + nextN.X
	by := prevN.Y + nextN.Y
	denom := 1 + prevN.X*nextN.X + prevN.Y*nextN.Y
	if denom < 1e-12 {
		// Anti-parallel normals — fall back to two perpendicular points.
		if emitAlways {
			a := Point{X: v.X + d*prevN.X, Y: v.Y + d*prevN.Y}
			c := Point{X: v.X + d*nextN.X, Y: v.Y + d*nextN.Y}
			*out = append(*out, a, c)
		}
		return
	}
	q := d / denom
	*out = append(*out, Point{X: v.X + bx*q, Y: v.Y + by*q})
}

// appendMiter emits a miter join: the apex of the two offset edges'
// intersection, unless the miter length exceeds miterLimit·|d|, in
// which case it falls back to a chamfer (two perpendicular points a, c).
func appendMiter(out *Polygon, v, a, c, prevN, nextN Point, d, miterLimit float64) {
	bx := prevN.X + nextN.X
	by := prevN.Y + nextN.Y
	denom := 1 + prevN.X*nextN.X + prevN.Y*nextN.Y
	if denom < 1e-12 {
		*out = append(*out, a, c)
		return
	}
	q := d / denom
	tx := bx * q
	ty := by * q
	if math.Hypot(tx, ty) > miterLimit*math.Abs(d) {
		// Chamfer.
		*out = append(*out, a, c)
		return
	}
	*out = append(*out, Point{X: v.X + tx, Y: v.Y + ty})
}

// appendSquareJoin emits a 45° chamfer at v: each offset endpoint is
// extended by |d| along its edge's tangent (away from v), giving a
// pentagon-style square corner. The result is two output points
// (a_ext, c_ext) inserted between adjacent offset edges.
func appendSquareJoin(out *Polygon, _, a, c, prevN, nextN Point, d float64) {
	// Tangent of prev edge in its direction (perpendicular to right-hand
	// normal): (-pny, pnx). At endpoint a, extending in this tangent moves
	// AWAY from v.
	absD := math.Abs(d)
	aExt := Point{X: a.X + absD*(-prevN.Y), Y: a.Y + absD*prevN.X}
	cExt := Point{X: c.X - absD*(-nextN.Y), Y: c.Y - absD*nextN.X}
	*out = append(*out, a, aExt, cExt, c)
}

// appendRoundJoin tessellates a circular arc of radius |d| from offset
// endpoint a to endpoint c, centred at v. The arc goes in the
// direction that fills the wedge gap: CCW for d>0, CW for d<0. Chord
// deviation per segment stays within arcTol; minimum 2 segments so
// the arc is non-degenerate.
func appendRoundJoin(out *Polygon, v, a, c Point, d, arcTol float64) {
	startAng := math.Atan2(a.Y-v.Y, a.X-v.X)
	endAng := math.Atan2(c.Y-v.Y, c.X-v.X)
	sweep := endAng - startAng
	if d >= 0 {
		for sweep <= 0 {
			sweep += 2 * math.Pi
		}
	} else {
		for sweep >= 0 {
			sweep -= 2 * math.Pi
		}
	}
	r := math.Abs(d)
	if arcTol <= 0 {
		arcTol = r * 0.01
	}
	cosVal := 1 - arcTol/r
	if cosVal <= -1 {
		// Single big step.
		*out = append(*out, a, c)
		return
	}
	maxStep := 2 * math.Acos(cosVal)
	segs := max(int(math.Ceil(math.Abs(sweep)/maxStep)), 2)
	*out = append(*out, a)
	step := sweep / float64(segs)
	for i := 1; i < segs; i++ {
		ang := startAng + step*float64(i)
		*out = append(*out, Point{X: v.X + r*math.Cos(ang), Y: v.Y + r*math.Sin(ang)})
	}
	*out = append(*out, c)
}

// ringsIntersect reports whether any edge of the offset rings properly
// crosses or collinearly overlaps another (ignoring the shared endpoint of
// consecutive edges within a ring). True means the offset changed topology —
// a pinched neck, a closing notch, an inside-out collapse, or two rings that
// now touch — and the piece must be re-resolved by [selfUnionPositive].
// Offset rings are short, so the O(n²) edge-pair scan is cheap.
func ringsIntersect(rings []Polygon) bool {
	type edge struct {
		a, b Point
		ring int
		idx  int
	}
	var edges []edge
	for r, ring := range rings {
		n := len(ring)
		for i := range n {
			edges = append(edges, edge{a: ring[i], b: ring[(i+1)%n], ring: r, idx: i})
		}
	}
	for i := range edges {
		for j := i + 1; j < len(edges); j++ {
			ei, ej := edges[i], edges[j]
			// Skip consecutive edges of the same ring (they legitimately share
			// one endpoint); any other shared geometry is a real intersection.
			if ei.ring == ej.ring {
				n := len(rings[ei.ring])
				if ei.idx == (ej.idx+1)%n || ej.idx == (ei.idx+1)%n {
					continue
				}
			}
			if segmentsProperlyIntersect(ei.a, ei.b, ej.a, ej.b) {
				return true
			}
		}
	}
	return false
}

// segmentsProperlyIntersect reports whether segments p1p2 and p3p4 share any
// point other than a single shared endpoint — i.e. they cross transversally
// or overlap collinearly, or one endpoint lies in the open interior of the
// other segment. A bare touch at a single common endpoint returns false.
func segmentsProperlyIntersect(p1, p2, p3, p4 Point) bool {
	d1 := orient(p3, p4, p1)
	d2 := orient(p3, p4, p2)
	d3 := orient(p1, p2, p3)
	d4 := orient(p1, p2, p4)
	if ((d1 > 0) != (d2 > 0)) && ((d3 > 0) != (d4 > 0)) &&
		d1 != 0 && d2 != 0 && d3 != 0 && d4 != 0 {
		return true // transversal crossing
	}
	// Collinear / endpoint-on-segment cases.
	if d1 == 0 && onSegmentInterior(p3, p4, p1) {
		return true
	}
	if d2 == 0 && onSegmentInterior(p3, p4, p2) {
		return true
	}
	if d3 == 0 && onSegmentInterior(p1, p2, p3) {
		return true
	}
	if d4 == 0 && onSegmentInterior(p1, p2, p4) {
		return true
	}
	return false
}

// orient returns the sign of the cross product (b-a)×(c-a): >0 left turn, <0
// right turn, 0 collinear.
func orient(a, b, c Point) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

// onSegmentInterior reports whether collinear point p lies strictly inside
// segment ab (not at either endpoint).
func onSegmentInterior(a, b, p Point) bool {
	if p == a || p == b {
		return false
	}
	return p.X >= min(a.X, b.X) && p.X <= max(a.X, b.X) &&
		p.Y >= min(a.Y, b.Y) && p.Y <= max(a.Y, b.Y)
}

// selfUnionResolveAngles lists the frame rotations tried by [selfUnionPositive].
// 0 first: a non-degenerate offset resolves exactly with no rotation, so its
// output vertices stay on their true positions. The oblique angles handle the
// degenerate geometry an axis-aligned (or thin-neck) inward offset produces:
// parallel walls a multiple of 2|d| apart, plus near-pinch self-crossings, snap
// to fragile configurations the boolean sweep resolves differently per frame.
// Rotating to an oblique frame snaps those features to distinct grid lines so
// the self-intersections become clean transversal crossings. The angles are
// spread across (0, π/2) avoiding axis (0, π/2) and the 45° diagonal (π/4),
// which themselves induce coincidences.
var selfUnionResolveAngles = []float64{
	0, 0.21, 0.43, 0.61, 0.92, 1.13, 1.32, 1.49,
}

// selfUnionPositive resolves a set of raw offset rings into clean output pieces
// via the scanline engine with the positive fill rule (DESIGN.md §7.1): the
// outer ring winds positively inside, hole rings negatively, so the printable
// region is exactly where the combined winding is strictly positive.
//
// The sweep is exact on transversal self-intersections but resolves a
// snapped degenerate configuration (same-source collinear coincident edges, or
// a near-pinch crossing) differently — and sometimes wrongly — depending on the
// coordinate frame. Such degeneracies are common in inward offsets of
// axis-aligned or thin-neck features. To be robust, the resolution is attempted
// in several rotated frames ([selfUnionResolveAngles]) and the most-agreed-upon
// result is chosen: the correct resolution recurs across frames (same piece
// count and area), while each degenerate misresolution is scattered. Among the
// agreeing majority the un-rotated (angle 0) result is preferred so a
// non-degenerate offset keeps its exact coordinates. Returns nil if every
// attempt fails or the region is empty.
func selfUnionPositive(rings []Polygon) MultiPolygon {
	bb := bboxOf(rings)
	cx, cy := (bb.Min.X+bb.Max.X)/2, (bb.Min.Y+bb.Max.Y)/2

	type cand struct {
		res    MultiPolygon
		area   float64
		pieces int
		zero   bool // produced at angle 0 (exact coordinates)
	}
	var cands []cand
	for _, ang := range selfUnionResolveAngles {
		res := selfUnionAt(rings, cx, cy, ang)
		if res == nil {
			continue
		}
		cands = append(cands, cand{res: res, area: res.Area(), pieces: len(res), zero: ang == 0})
	}
	if len(cands) == 0 {
		return nil
	}
	// Agreement score: number of candidates with the same piece count and an
	// area within 2% (degenerate misresolutions rarely cluster).
	agree := func(a, b cand) bool {
		if a.pieces != b.pieces {
			return false
		}
		den := max(a.area, b.area)
		if den == 0 {
			return true
		}
		return math.Abs(a.area-b.area)/den <= 0.02
	}
	best := 0
	bestScore := -1
	for i := range cands {
		score := 0
		for j := range cands {
			if agree(cands[i], cands[j]) {
				score++
			}
		}
		switch {
		case score > bestScore:
			best, bestScore = i, score
		case score == bestScore && cands[i].zero && !cands[best].zero:
			best = i // prefer exact (un-rotated) coordinates within the majority
		}
	}
	return cands[best].res
}

// selfUnionAt rotates rings about (cx,cy) by ang, runs the positive-fill
// self-union, and rotates the result back. ang == 0 skips both rotations so
// the output coordinates are exactly the input ones.
func selfUnionAt(rings []Polygon, cx, cy, ang float64) MultiPolygon {
	work := rings
	ca, sa := 1.0, 0.0
	if ang != 0 {
		ca, sa = math.Cos(ang), math.Sin(ang)
		work = make([]Polygon, len(rings))
		for i, r := range rings {
			rr := make(Polygon, len(r))
			for j, p := range r {
				rr[j] = rotateAbout(p, cx, cy, ca, sa)
			}
			work[i] = rr
		}
	}

	bb := bboxOf(work)
	scale := fixed.ScaleFromBBox(bb.Min.X, bb.Min.Y, bb.Max.X, bb.Max.Y)
	orderedRings := make([][]clip.Segment, 0, len(work))
	for _, r := range work {
		var rs []clip.Segment
		appendOffsetRingSegs(&rs, r, scale)
		if len(rs) > 0 {
			orderedRings = append(orderedRings, rs)
		}
	}
	sw := clip.SweepRingsFill(orderedRings, clip.OpUnion, clip.FillPositive)
	if sw.Err != nil {
		return nil
	}
	res := assembleResult(sw.Rings, scale)
	if ang != 0 {
		for _, ex := range res {
			for i, p := range ex.Outer {
				ex.Outer[i] = rotateAbout(p, cx, cy, ca, -sa)
			}
			for _, h := range ex.Holes {
				for i, p := range h {
					h[i] = rotateAbout(p, cx, cy, ca, -sa)
				}
			}
		}
	}
	return res
}

// rotateAbout rotates p about (cx,cy) by the rotation with cosine ca and sine
// sa (negate sa to invert).
func rotateAbout(p Point, cx, cy, ca, sa float64) Point {
	dx, dy := p.X-cx, p.Y-cy
	return Point{X: cx + dx*ca - dy*sa, Y: cy + dx*sa + dy*ca}
}

// bboxOf returns the bounding box of all vertices across rings.
func bboxOf(rings []Polygon) BBox {
	var bb BBox
	first := true
	for _, r := range rings {
		for _, p := range r {
			if first {
				bb = BBox{Min: p, Max: p}
				first = false
				continue
			}
			bb.Min.X = min(bb.Min.X, p.X)
			bb.Min.Y = min(bb.Min.Y, p.Y)
			bb.Max.X = max(bb.Max.X, p.X)
			bb.Max.Y = max(bb.Max.Y, p.Y)
		}
	}
	return bb
}

// appendOffsetRingSegs snaps ring to the grid and emits its edges as subject
// segments in input order — unlike boolean.appendRing it does NOT normalize
// orientation, since the offset self-union relies on the natural traversal
// direction to set the winding sign.
func appendOffsetRingSegs(dst *[]clip.Segment, ring Polygon, scale fixed.Scale) {
	n := len(ring)
	if n < 3 {
		return
	}
	pts := make([]fixed.Point, 0, n)
	for i := range n {
		p := scale.Snap(ring[i].X, ring[i].Y)
		if len(pts) > 0 && pts[len(pts)-1] == p {
			continue
		}
		pts = append(pts, p)
	}
	for len(pts) >= 2 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	pts = simplifyCollinearRing(pts)
	m := len(pts)
	if m < 3 {
		return
	}
	for i := range m {
		seg := clip.NewSegment(pts[i], pts[(i+1)%m], clip.Subject)
		if !seg.Degenerate() {
			*dst = append(*dst, seg)
		}
	}
}

func cloneMulti(m MultiPolygon) MultiPolygon {
	out := make(MultiPolygon, len(m))
	for i, ex := range m {
		outer := make(Polygon, len(ex.Outer))
		copy(outer, ex.Outer)
		holes := make([]Polygon, len(ex.Holes))
		for j, h := range ex.Holes {
			holes[j] = make(Polygon, len(h))
			copy(holes[j], h)
		}
		out[i] = ExPolygon{Outer: outer, Holes: holes}
	}
	return out
}
