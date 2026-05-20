package polyclip

import (
	"errors"
	"math"
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
// Hole orientation: outer rings are CCW, holes are CW (the standard
// polyclip convention). A positive d inflates outer rings and shrinks
// holes; a negative d does the opposite. When a hole closes up under
// inward offset (its width drops below 2·|d|) it is dropped; when an
// outer ring vanishes the whole piece is dropped.
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
		// Outer ring: offset by d. Right-hand normal of a CCW ring points
		// outward, so positive d grows the ring outward.
		outer := offsetRing(ex.Outer, d, opts)
		if !validOffsetRing(outer, true) {
			// Outer collapsed (inward offset > smallest half-extent) — drop
			// the whole piece.
			continue
		}
		piece := ExPolygon{Outer: outer}
		// Holes: a CW hole walked CW has its right-hand normal pointing
		// INTO the hole. Offsetting the hole boundary by +d (with the same
		// formula) moves it INTO the hole — which GROWS the printable
		// region. To grow the printable region for d>0 we want holes to
		// shrink, so offset holes by -d.
		for _, h := range ex.Holes {
			offHole := offsetRing(h, -d, opts)
			if !validOffsetRing(offHole, false) {
				// Hole collapsed — drop it (the printable region absorbs it).
				continue
			}
			piece.Holes = append(piece.Holes, offHole)
		}
		result = append(result, piece)
	}
	if len(result) == 0 {
		return nil, ErrOffsetEmpty
	}
	return result, nil
}

// validOffsetRing reports whether an offset ring kept its expected
// orientation. An outer ring is valid only if CCW (positive signed
// area); a hole only if CW (negative signed area). A ring that flipped
// orientation collapsed under inward offset and should be discarded.
// Length < 3 is also invalid.
func validOffsetRing(ring Polygon, outer bool) bool {
	if len(ring) < 3 {
		return false
	}
	area := ring.SignedArea()
	if outer {
		return area > 0
	}
	return area < 0
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
	// For inward offset (d<0), detect overshoot: the offset region is the
	// intersection of half-planes (V - ring[i]) · normals[i] ≤ d for every
	// input edge i. If any output vertex violates any of these half-plane
	// constraints by more than the tessellation tolerance, the offset has
	// overshot the inradius and the ring is invalid (geometrically
	// collapsed even if its signed area is still positive — see the
	// inside-out 2x2 result for d=-6 on a 10x10 square).
	if d < 0 && !inwardRingValid(out, ring, normals, d, opts.ArcTol) {
		return nil
	}
	return out
}

// inwardRingValid returns true when every output vertex satisfies every
// input-edge inward half-plane constraint, modulo a tolerance that
// absorbs chord-deviation noise from round joins. Used only for d<0;
// for d>0 the offset never collapses.
//
// Tolerance: max(arcTol, |d|·1e-6). When the input itself came from a
// round-joined outward offset, its arc chords lie up to arcTol inside
// the true arc, so a valid inward apex can appear to violate a chord-
// edge constraint by up to arcTol. The 1e-6·|d| floor handles
// non-arc inputs where only floating-point noise is at play.
func inwardRingValid(out Polygon, ring Polygon, normals []Point, d, arcTol float64) bool {
	tol := math.Abs(d) * 1e-6
	if arcTol > tol {
		tol = arcTol
	}
	for _, v := range out {
		for i, p := range ring {
			n := normals[i]
			if n == (Point{}) {
				continue // degenerate edge — no constraint.
			}
			dot := (v.X-p.X)*n.X + (v.Y-p.Y)*n.Y
			if dot > d+tol {
				return false
			}
		}
	}
	return true
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
func appendSquareJoin(out *Polygon, v, a, c, prevN, nextN Point, d float64) {
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
	segs := int(math.Ceil(math.Abs(sweep) / maxStep))
	if segs < 2 {
		segs = 2
	}
	*out = append(*out, a)
	step := sweep / float64(segs)
	for i := 1; i < segs; i++ {
		ang := startAng + step*float64(i)
		*out = append(*out, Point{X: v.X + r*math.Cos(ang), Y: v.Y + r*math.Sin(ang)})
	}
	*out = append(*out, c)
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
