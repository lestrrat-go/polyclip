package clip

import (
	"errors"
	"fmt"
	"math"
	"sort"

	"github.com/lestrrat-go/polyclip/fixed"
)

// Bound is a chain of segments traversed monotonically in Y (non-decreasing).
// A Bound starts at a local minimum and ends at a local maximum of its
// input ring. Per DESIGN.md §12.1, a Bound is the AEL-entry unit in the
// bound model: an [ActiveEdge] cursors through Bound.Segs via
// UpdateEdgeIntoAEL as the scanline advances.
//
// Segs is in ascending-Y order. Each segment is canonical (Bot.Y < Top.Y
// for non-horizontals, Bot.X < Top.X for horizontals). The cursor advances
// from index 0 (local-min end) to len(Segs)-1 (local-max end).
type Bound struct {
	Segs []*Segment
}

// First returns the first segment of the bound (the one starting at the
// local minimum), or nil if the bound is empty.
func (b *Bound) First() *Segment {
	if b == nil || len(b.Segs) == 0 {
		return nil
	}
	return b.Segs[0]
}

// Last returns the last segment of the bound (the one ending at the local
// maximum), or nil if the bound is empty.
func (b *Bound) Last() *Segment {
	if b == nil || len(b.Segs) == 0 {
		return nil
	}
	return b.Segs[len(b.Segs)-1]
}

// LocalMin is a local minimum of an input ring: a vertex where the Y
// direction reverses from descending to ascending, with two ascending
// bounds emerging upward. Per DESIGN.md §12.1 / §12.7.
//
// Left is the bound that, at the local-minimum scanline, sits to the LEFT
// in the active edge list (lower CurrX, or same CurrX with smaller slope).
// Right is the bound to the right. For a CCW outer ring traced in input
// order, Right is the bound walking the ring CCW forward; Left walks
// backward.
type LocalMin struct {
	Vertex fixed.Point
	Left   *Bound
	Right  *Bound
}

// ErrOpenRing is returned by [BuildLocalMinima] when the input segments do
// not form closed rings — typically the result of an open chain or a
// shared vertex that breaks ring topology reconstruction.
var ErrOpenRing = errors.New("clip: input segments do not form closed rings")

// BuildLocalMinima reconstructs ring topology from segs and returns the
// local minima of every input ring, sorted by vertex (Y ascending, X
// ascending for ties). Each minimum carries two ascending bounds; the
// bounds together cover every non-degenerate segment in the input exactly
// once.
//
// Ring reconstruction uses input-direction adjacency — segment B follows
// segment A iff A.End() == B.Start(). For non-self-intersecting input
// with no shared vertices between rings this is unambiguous. Shared
// vertices return [ErrOpenRing] (the map lookup overwrites, then the
// walker discovers a topology violation).
//
// Mid-bound horizontals (staircase steps) are part of bounds — they do
// NOT create local minima or maxima. Horizontals at the bottom of a
// polygon contribute to the local-min bound's first segment(s); at the
// top, to the bound's last segment(s).
func BuildLocalMinima(segs []Segment) ([]LocalMin, error) {
	// byStart is a map from each input-direction Start vertex to the list
	// of segments starting there. Shared vertices (two rings touching at a
	// corner) produce a list of length > 1; [traceRing] disambiguates with
	// source + angle.
	byStart := make(map[fixed.Point][]*Segment, len(segs))
	for i := range segs {
		s := &segs[i]
		if s.Degenerate() {
			continue
		}
		byStart[s.Start()] = append(byStart[s.Start()], s)
	}

	visited := make(map[*Segment]struct{}, len(segs))
	var minima []LocalMin
	for i := range segs {
		s := &segs[i]
		if s.Degenerate() {
			continue
		}
		if _, seen := visited[s]; seen {
			continue
		}
		ring, err := traceRing(s, byStart, visited)
		if err != nil {
			return nil, err
		}
		ringMins, err := findRingMinima(ring)
		if err != nil {
			return nil, err
		}
		minima = append(minima, ringMins...)
	}

	sort.Slice(minima, func(i, j int) bool {
		return LessYX(minima[i].Vertex, minima[j].Vertex)
	})
	return minima, nil
}

// traceRing walks from start following input-direction End→Start links
// until it returns to start. All visited segments are marked. Returns
// [ErrOpenRing] if the chain breaks or visits a non-start segment twice.
//
// At a vertex where multiple segments start (shared corner between two
// touching rings), the next segment is chosen by: (1) source match —
// prefer same-Src as the incoming segment, then (2) smallest CCW turn
// from the incoming direction, which keeps the walk within a single ring
// when two rings of the same source touch at a vertex.
func traceRing(start *Segment, byStart map[fixed.Point][]*Segment, visited map[*Segment]struct{}) ([]*Segment, error) {
	ring := []*Segment{start}
	visited[start] = struct{}{}
	cur := start
	for {
		candidates := byStart[cur.End()]
		next := pickNextSegment(cur, candidates, visited)
		if next == nil {
			return nil, fmt.Errorf("%w: chain breaks at vertex %v (no outgoing segment)", ErrOpenRing, cur.End())
		}
		if next == start {
			return ring, nil
		}
		visited[next] = struct{}{}
		ring = append(ring, next)
		cur = next
	}
}

// pickNextSegment chooses the next segment of cur's ring at vertex
// cur.End() from the candidate list. Filter rules:
//
//  1. Drop already-visited segments (those are part of completed rings),
//     unless one of them is the start of the ring being traced.
//  2. Among the survivors, prefer same-Src as cur.
//  3. If still ambiguous, pick the candidate with the smallest CCW turn
//     from cur's input direction.
func pickNextSegment(cur *Segment, candidates []*Segment, visited map[*Segment]struct{}) *Segment {
	var matches []*Segment
	for _, c := range candidates {
		if c == cur {
			continue
		}
		if _, seen := visited[c]; seen {
			// Allow re-visit only if c is the start of the ring being
			// traced — that's how traceRing detects ring closure. Since
			// traceRing already checks `next == start`, we don't need to
			// special-case here: include visited candidates and let the
			// outer loop catch the closure. But filter out OTHER visited
			// (non-start) segs to avoid jumping into a different ring.
			matches = append(matches, c)
			continue
		}
		matches = append(matches, c)
	}
	if len(matches) == 0 {
		return nil
	}
	if len(matches) == 1 {
		return matches[0]
	}
	// Multiple candidates — prefer same-Src.
	var sameSrc []*Segment
	for _, c := range matches {
		if c.Src == cur.Src {
			sameSrc = append(sameSrc, c)
		}
	}
	if len(sameSrc) == 1 {
		return sameSrc[0]
	}
	pool := matches
	if len(sameSrc) > 1 {
		pool = sameSrc
	}
	// Smallest CCW turn from cur's incoming direction. The "turn angle"
	// is the angle from (-cur_dir) to candidate_dir measured CCW. For two
	// rings of the same source touching at a vertex, the candidate from
	// the same ring forms the smaller CCW turn (the ring continues on the
	// same side).
	incoming := vec(cur.End(), cur.Start()) // reversed: points BACK along cur
	bestIdx := -1
	bestAngle := 0.0
	for i, c := range pool {
		out := vec(c.Start(), c.End())
		ang := ccwAngleFrom(incoming, out)
		if bestIdx < 0 || ang < bestAngle {
			bestIdx = i
			bestAngle = ang
		}
	}
	return pool[bestIdx]
}

func vec(from, to fixed.Point) [2]float64 {
	return [2]float64{
		float64(int64(to.X) - int64(from.X)),
		float64(int64(to.Y) - int64(from.Y)),
	}
}

// ccwAngleFrom returns the CCW angle in [0, 2π) from vector u to vector
// v. Used to choose the "innermost" outgoing segment at a vertex with
// multiple ring branches.
func ccwAngleFrom(u, v [2]float64) float64 {
	const twoPi = 2 * 3.141592653589793
	a := atan2(v[1], v[0]) - atan2(u[1], u[0])
	for a < 0 {
		a += twoPi
	}
	for a >= twoPi {
		a -= twoPi
	}
	return a
}

func atan2(y, x float64) float64 { return math.Atan2(y, x) }

// findRingMinima identifies every local minimum in ring and builds its two
// ascending bounds. ring is in input-direction order and forms a closed
// cycle. Returns one LocalMin per down→up transition (skipping horizontal
// edges per DESIGN.md §12.7).
func findRingMinima(ring []*Segment) ([]LocalMin, error) {
	n := len(ring)
	if n == 0 {
		return nil, nil
	}

	dirs := make([]yDir, n)
	anyNonFlat := false
	for i, s := range ring {
		dirs[i] = yDirection(s)
		if dirs[i] != yFlat {
			anyNonFlat = true
		}
	}
	if !anyNonFlat {
		// Entirely-horizontal ring is degenerate (zero area). No minima.
		return nil, nil
	}

	// For each index i, compute the previous non-flat direction (walking
	// backward, wrapping) and the next non-flat direction (walking forward,
	// wrapping). A local minimum is at the end-vertex of an edge whose Y
	// direction is yDown AND whose effective next non-flat direction is
	// yUp.
	var minima []LocalMin
	for i := range ring {
		if dirs[i] != yDown {
			continue
		}
		nextI := nextNonFlat(dirs, i)
		if nextI < 0 || dirs[nextI] != yUp {
			continue
		}
		v := ring[i].End() // input-direction end of the descending edge
		bounds, err := buildBoundsAt(ring, dirs, i)
		if err != nil {
			return nil, err
		}
		minima = append(minima, LocalMin{
			Vertex: v,
			Left:   bounds.left,
			Right:  bounds.right,
		})
	}
	return minima, nil
}

type pairOfBounds struct {
	left, right *Bound
}

// buildBoundsAt constructs the two ascending bounds emerging from the
// local minimum between ring[downIdx] (descending edge ending at the local
// min) and ring[upIdx] (first ascending edge after any flats).
//
//   - The Right bound walks the ring forward in input direction starting
//     from ring[(downIdx+1)%n] (which may be flat or up) and continuing
//     through every up/flat edge up to and including the last edge before
//     the next descending edge (i.e. the local maximum).
//   - The Left bound walks the ring backward in input direction starting
//     from ring[downIdx] reversed and continuing backward until reaching
//     the same local maximum from the other side.
//
// Both bound Segs slices are in ascending-Y order.
func buildBoundsAt(ring []*Segment, dirs []yDir, downIdx int) (pairOfBounds, error) {
	n := len(ring)

	// Right bound: walk forward from (downIdx+1) up to but not past the
	// next yDown, then trim trailing horizontals — those belong to the
	// local-max plateau which is part of the descending side.
	var rightSegs []*Segment
	for k, idx := 0, (downIdx+1)%n; k < n; k, idx = k+1, (idx+1)%n {
		if dirs[idx] == yDown {
			break
		}
		rightSegs = append(rightSegs, ring[idx])
	}
	// Find the position of the last yUp in rightSegs and truncate after it.
	lastUp := -1
	for i, s := range rightSegs {
		if yDirection(s) == yUp {
			lastUp = i
		}
	}
	if lastUp < 0 {
		return pairOfBounds{}, fmt.Errorf("%w: Right bound from local min at %v has no ascending edge",
			ErrOpenRing, ring[downIdx].End())
	}
	rightSegs = rightSegs[:lastUp+1]
	right := &Bound{Segs: rightSegs}

	// Left bound: walk BACKWARD from downIdx (visiting input-direction yDown
	// and yFlat edges) until the first yUp. The visit order is already
	// ascending-Y (we walk from local-min level up toward the local-max
	// plateau), so append-as-we-go yields the correct order.
	var leftSegs []*Segment
	for k, idx := 0, downIdx; k < n; k, idx = k+1, (idx-1+n)%n {
		if dirs[idx] == yUp {
			break
		}
		leftSegs = append(leftSegs, ring[idx])
	}
	if len(leftSegs) == 0 {
		return pairOfBounds{}, fmt.Errorf("%w: Left bound from local min at %v is empty",
			ErrOpenRing, ring[downIdx].End())
	}
	left := &Bound{Segs: leftSegs}

	leftSide, rightSide := orientBounds(left, right, ring[downIdx].End())
	return pairOfBounds{left: leftSide, right: rightSide}, nil
}

// orientBounds returns (leftBound, rightBound) ordered as they appear in
// the AEL at the local-min Y. The decision uses the first non-horizontal
// edge of each bound: the bound whose first non-horizontal has smaller X
// AT THE NEXT SCANLINE goes to the left.
//
// For axial polygons the leading horizontals end at the verticals' Bot.X,
// and the verticals have constant X. For sloped edges we compute slope at
// the local-min vertex.
func orientBounds(a, b *Bound, localMin fixed.Point) (left, right *Bound) {
	aX := boundInitialX(a, localMin)
	bX := boundInitialX(b, localMin)
	if aX < bX {
		return a, b
	}
	if bX < aX {
		return b, a
	}
	// CurrX equal — slope tie-break (smaller slope to the left).
	aSlope := boundInitialSlope(a)
	bSlope := boundInitialSlope(b)
	if aSlope < bSlope {
		return a, b
	}
	return b, a
}

// boundInitialX returns the X position of the bound's first non-horizontal
// edge AT the local-min Y. For a leading horizontal whose far end starts
// the first non-horizontal, that far-end X is what enters the AEL.
func boundInitialX(b *Bound, localMin fixed.Point) fixed.Coord {
	for _, s := range b.Segs {
		if !s.Horizontal() {
			return s.Bot.X
		}
	}
	// All-horizontal bound shouldn't occur for a valid ring; fall back to
	// local-min X.
	return localMin.X
}

func boundInitialSlope(b *Bound) float64 {
	for _, s := range b.Segs {
		if !s.Horizontal() {
			return slope(s)
		}
	}
	return 0
}

// nextNonFlat returns the smallest index k > i (mod n) such that dirs[k]
// is not yFlat. Returns -1 if the entire ring is flat.
func nextNonFlat(dirs []yDir, i int) int {
	n := len(dirs)
	for k := 1; k <= n; k++ {
		j := (i + k) % n
		if dirs[j] != yFlat {
			return j
		}
	}
	return -1
}
