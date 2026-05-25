package polyclip

import (
	"math"
	"slices"
	"sort"
)

// Triangle is a single triangle of a triangulation: its three corners wound
// counter-clockwise.
type Triangle [3]Point

// Triangulate decomposes m into a flat list of triangles covering exactly the
// same region. Each [ExPolygon] is triangulated independently and the results
// are concatenated; the order of triangles is otherwise unspecified.
//
// The triangulation is a faithful, from-scratch implementation of the ear-
// clipping algorithm with hole elimination (the approach popularized by
// mapbox/earcut), chosen because Clipper2's own triangulation module is known
// to mis-handle several cases. Holes are connected to their outer ring by
// bridge edges, then the resulting polygon is triangulated by ear clipping with
// self-intersection cures and recursive splitting as robustness fallbacks. The
// output uses only the input's own vertices — no Steiner points are introduced
// — and every triangle is wound counter-clockwise. Zero-area (collinear)
// triangles are dropped.
//
// Triangulate is purely geometric: it does not run the boolean engine. Input
// winding need not be normalized (it is corrected internally). For best results
// m should be well-formed — outer rings simple, holes simple, non-overlapping
// and inside their outer. This is the form [Simplify] produces; pass self-
// intersecting input, or raw output whose holes may pinch against the outer
// boundary, through [Simplify] first. Rings with fewer than three vertices
// contribute nothing.
func Triangulate(m MultiPolygon) []Triangle {
	var out []Triangle
	for i := range m {
		out = appendExPolygonTriangles(out, m[i])
	}
	return out
}

// tnode is one vertex of a doubly-linked polygon ring used during ear clipping.
type tnode struct {
	pt         Point
	prev, next *tnode
	steiner    bool
}

func appendExPolygonTriangles(out []Triangle, ex ExPolygon) []Triangle {
	if len(ex.Outer) < 3 {
		return out
	}
	// Outer ring CCW, holes CW (the library convention the algorithm expects).
	outer := buildRing(ex.Outer, true)
	if outer == nil || outer.next == outer.prev {
		return out
	}
	if len(ex.Holes) > 0 {
		outer = eliminateHoles(ex.Holes, outer)
	}
	return earcutLinked(out, outer, 0)
}

// buildRing creates a circular doubly-linked list from ring, oriented to the
// requested winding (the algorithm wants outer rings CCW and holes CW, the same
// convention as the rest of the library). Returns the last inserted node, or
// nil if the ring is empty.
func buildRing(ring Polygon, wantCCW bool) *tnode {
	if len(ring) == 0 {
		return nil
	}
	ccw := ring.SignedArea() > 0 // shoelace > 0 ⇒ counter-clockwise
	var last *tnode
	if ccw == wantCCW {
		for _, p := range ring {
			last = insertNode(p, last)
		}
	} else {
		for _, p := range slices.Backward(ring) {
			last = insertNode(p, last)
		}
	}
	if last != nil && equalsNode(last, last.next) {
		removeNode(last)
		last = last.next
	}
	return last
}

func insertNode(pt Point, last *tnode) *tnode {
	n := &tnode{pt: pt}
	if last == nil {
		n.prev = n
		n.next = n
		return n
	}
	n.next = last.next
	n.prev = last
	last.next.prev = n
	last.next = n
	return n
}

func removeNode(n *tnode) {
	n.next.prev = n.prev
	n.prev.next = n.next
}

func equalsNode(a, b *tnode) bool {
	return a.pt == b.pt
}

// earcutLinked triangulates the ring rooted at ear, appending triangles to out.
// pass drives the robustness ladder: 0 plain, 1 after removing degenerate
// points, 2 after curing local self-intersections, 3 via recursive splitting.
func earcutLinked(out []Triangle, ear *tnode, pass int) []Triangle {
	if ear == nil {
		return out
	}
	stop := ear
	for ear.prev != ear.next {
		prev := ear.prev
		next := ear.next
		if isEarNode(ear) {
			out = emitTriangle(out, prev, ear, next)
			removeNode(ear)
			ear = next.next
			stop = next.next
			continue
		}
		ear = next
		if ear != stop {
			continue
		}
		// No ear found in a full lap: escalate through the fallbacks.
		switch pass {
		case 0:
			return earcutLinked(out, filterPoints(ear, nil), 1)
		case 1:
			ear = filterPoints(ear, nil)
			out, ear = cureLocalIntersections(out, ear)
			return earcutLinked(out, ear, 2)
		default:
			return splitEarcut(out, ear)
		}
	}
	return out
}

// isEarNode reports whether ear is a convex vertex whose triangle contains no
// reflex vertex of the ring (the standard ear test).
func isEarNode(ear *tnode) bool {
	a, b, c := ear.prev, ear, ear.next
	if nodeArea(a.pt, b.pt, c.pt) >= 0 {
		return false // reflex or flat: not an ear
	}
	for p := c.next; p != a; p = p.next {
		if pointInTri(a.pt, b.pt, c.pt, p.pt) &&
			nodeArea(p.prev.pt, p.pt, p.next.pt) >= 0 {
			return false
		}
	}
	return true
}

// cureLocalIntersections removes self-intersections that arise from coincident
// bridge vertices by clipping the offending pair of edges into a triangle.
func cureLocalIntersections(out []Triangle, start *tnode) ([]Triangle, *tnode) {
	p := start
	for {
		a := p.prev
		b := p.next.next
		if !equalsNode(a, b) && intersects(a.pt, p.pt, p.next.pt, b.pt) &&
			locallyInside(a, b) && locallyInside(b, a) {
			out = emitTriangle(out, a, p, b)
			removeNode(p)
			removeNode(p.next)
			p = b
			start = b
		}
		p = p.next
		if p == start {
			break
		}
	}
	return out, filterPoints(p, nil)
}

// splitEarcut breaks a polygon that has no ears into two simpler polygons along
// a valid diagonal, then triangulates each — the last-resort fallback that
// guarantees progress on weakly-simple (bridged) polygons.
func splitEarcut(out []Triangle, start *tnode) []Triangle {
	a := start
	for {
		b := a.next.next
		for b != a.prev {
			if a != b && isValidDiagonal(a, b) {
				c := splitPolygon(a, b)
				a = filterPoints(a, a.next)
				c = filterPoints(c, c.next)
				out = earcutLinked(out, a, 0)
				out = earcutLinked(out, c, 0)
				return out
			}
			b = b.next
		}
		a = a.next
		if a == start {
			break
		}
	}
	return out
}

// filterPoints removes duplicate and collinear (zero-area) vertices from the
// ring between start and end, returning a still-valid node.
func filterPoints(start, end *tnode) *tnode {
	if start == nil {
		return start
	}
	if end == nil {
		end = start
	}
	p := start
	for {
		again := false
		if !p.steiner && (equalsNode(p, p.next) || nodeArea(p.prev.pt, p.pt, p.next.pt) == 0) {
			removeNode(p)
			p = p.prev
			end = p
			if p == p.next {
				break
			}
			again = true
		} else {
			p = p.next
		}
		if !again && p == end {
			break
		}
	}
	return end
}

// eliminateHoles connects every hole to the outer ring with a bridge, returning
// the merged outer ring. Holes are processed left-to-right by their leftmost
// vertex (earcut order).
func eliminateHoles(holes []Polygon, outerNode *tnode) *tnode {
	queue := make([]*tnode, 0, len(holes))
	for _, h := range holes {
		if len(h) < 3 {
			continue
		}
		list := buildRing(h, false)
		if list == nil {
			continue
		}
		if list == list.next {
			list.steiner = true
		}
		queue = append(queue, getLeftmost(list))
	}
	sort.Slice(queue, func(i, j int) bool {
		if queue[i].pt.X != queue[j].pt.X {
			return queue[i].pt.X < queue[j].pt.X
		}
		return queue[i].pt.Y < queue[j].pt.Y
	})
	for _, lm := range queue {
		outerNode = eliminateHole(lm, outerNode)
	}
	return outerNode
}

func eliminateHole(hole, outerNode *tnode) *tnode {
	bridge := findHoleBridge(hole, outerNode)
	if bridge == nil {
		return outerNode
	}
	bridgeReverse := splitPolygon(bridge, hole)
	filterPoints(bridgeReverse, bridgeReverse.next)
	return filterPoints(bridge, bridge.next)
}

func getLeftmost(start *tnode) *tnode {
	p := start
	leftmost := start
	for {
		if p.pt.X < leftmost.pt.X ||
			(p.pt.X == leftmost.pt.X && p.pt.Y < leftmost.pt.Y) {
			leftmost = p
		}
		p = p.next
		if p == start {
			break
		}
	}
	return leftmost
}

// findHoleBridge finds a point on the outer ring mutually visible to the hole's
// leftmost vertex, to use as the bridge connection. Returns nil if none exists.
func findHoleBridge(hole, outerNode *tnode) *tnode {
	p := outerNode
	hx, hy := hole.pt.X, hole.pt.Y
	qx := math.Inf(-1)
	var m *tnode

	// Cast a ray from the hole's leftmost vertex toward -x; track the rightmost
	// (closest) edge crossing to its left and the edge endpoint of larger x.
	for {
		if hy <= p.pt.Y && hy >= p.next.pt.Y && p.next.pt.Y != p.pt.Y {
			x := p.pt.X + (hy-p.pt.Y)*(p.next.pt.X-p.pt.X)/(p.next.pt.Y-p.pt.Y)
			if x <= hx && x > qx {
				qx = x
				m = p
				if p.next.pt.X > p.pt.X {
					m = p.next
				}
				if x == hx {
					return m // hole touches an outer vertex
				}
			}
		}
		p = p.next
		if p == outerNode {
			break
		}
	}
	if m == nil {
		return nil
	}

	// If the ray hits an edge interior, the visible vertex may be a reflex
	// vertex inside the triangle (hole point, ray hit, edge endpoint). Pick the
	// one of minimum angle to the ray.
	stop := m
	mx, my := m.pt.X, m.pt.Y
	tanMin := math.Inf(1)
	p = m
	for {
		var c1, c3 Point
		if hy < my {
			c1 = Point{X: hx, Y: hy}
			c3 = Point{X: qx, Y: hy}
		} else {
			c1 = Point{X: qx, Y: hy}
			c3 = Point{X: hx, Y: hy}
		}
		if hx >= p.pt.X && p.pt.X >= mx && hx != p.pt.X &&
			pointInTri(c1, Point{X: mx, Y: my}, c3, p.pt) {
			tan := math.Abs(hy-p.pt.Y) / (hx - p.pt.X)
			if locallyInside(p, hole) &&
				(tan < tanMin || (tan == tanMin && (p.pt.X > m.pt.X ||
					(p.pt.X == m.pt.X && sectorContainsSector(m, p))))) {
				m = p
				tanMin = tan
			}
		}
		p = p.next
		if p == stop {
			break
		}
	}
	return m
}

func sectorContainsSector(m, p *tnode) bool {
	return nodeArea(m.prev.pt, m.pt, p.prev.pt) < 0 && nodeArea(p.next.pt, m.pt, m.next.pt) < 0
}

// splitPolygon links a and b with a bridge, duplicating both into a2/b2 so the
// ring stays a single closed loop, and returns the new b2 node.
func splitPolygon(a, b *tnode) *tnode {
	a2 := &tnode{pt: a.pt}
	b2 := &tnode{pt: b.pt}
	an := a.next
	bp := b.prev

	a.next = b
	b.prev = a
	a2.next = an
	an.prev = a2
	b2.next = a2
	a2.prev = b2
	bp.next = b2
	b2.prev = bp
	return b2
}

func isValidDiagonal(a, b *tnode) bool {
	if a.next == b || a.prev == b || intersectsPolygon(a, b) {
		return false
	}
	locallyOK := locallyInside(a, b) && locallyInside(b, a) && middleInside(a, b) &&
		(nodeArea(a.prev.pt, a.pt, b.prev.pt) != 0 || nodeArea(a.pt, b.prev.pt, b.pt) != 0)
	if locallyOK {
		return true
	}
	// Zero-length diagonal between coincident vertices, both convex.
	return equalsNode(a, b) &&
		nodeArea(a.prev.pt, a.pt, a.next.pt) > 0 &&
		nodeArea(b.prev.pt, b.pt, b.next.pt) > 0
}

func intersectsPolygon(a, b *tnode) bool {
	p := a
	for {
		if p != a && p.next != a && p != b && p.next != b &&
			intersects(p.pt, p.next.pt, a.pt, b.pt) {
			return true
		}
		p = p.next
		if p == a {
			break
		}
	}
	return false
}

// intersects reports whether segments p1q1 and p2q2 intersect, including
// collinear-overlap and endpoint-touch cases.
func intersects(p1, q1, p2, q2 Point) bool {
	o1 := sign(nodeArea(p1, q1, p2))
	o2 := sign(nodeArea(p1, q1, q2))
	o3 := sign(nodeArea(p2, q2, p1))
	o4 := sign(nodeArea(p2, q2, q1))
	if o1 != o2 && o3 != o4 {
		return true
	}
	if o1 == 0 && onSeg(p1, p2, q1) {
		return true
	}
	if o2 == 0 && onSeg(p1, q2, q1) {
		return true
	}
	if o3 == 0 && onSeg(p2, p1, q2) {
		return true
	}
	if o4 == 0 && onSeg(p2, q1, q2) {
		return true
	}
	return false
}

// onSeg reports whether q lies on segment pr, assuming the three are collinear.
func onSeg(p, q, r Point) bool {
	return q.X <= max(p.X, r.X) && q.X >= min(p.X, r.X) &&
		q.Y <= max(p.Y, r.Y) && q.Y >= min(p.Y, r.Y)
}

// locallyInside reports whether b lies inside the cone of the polygon at a.
func locallyInside(a, b *tnode) bool {
	if nodeArea(a.prev.pt, a.pt, a.next.pt) < 0 {
		return nodeArea(a.pt, b.pt, a.next.pt) >= 0 && nodeArea(a.pt, a.prev.pt, b.pt) >= 0
	}
	return nodeArea(a.pt, b.pt, a.prev.pt) < 0 || nodeArea(a.pt, a.next.pt, b.pt) < 0
}

// middleInside reports whether the midpoint of a→b lies inside the polygon.
func middleInside(a, b *tnode) bool {
	p := a
	inside := false
	px := (a.pt.X + b.pt.X) / 2
	py := (a.pt.Y + b.pt.Y) / 2
	for {
		if (p.pt.Y > py) != (p.next.pt.Y > py) && p.next.pt.Y != p.pt.Y &&
			px < (p.next.pt.X-p.pt.X)*(py-p.pt.Y)/(p.next.pt.Y-p.pt.Y)+p.pt.X {
			inside = !inside
		}
		p = p.next
		if p == a {
			break
		}
	}
	return inside
}

// emitTriangle appends the triangle a,b,c to out as a CCW triangle, dropping it
// if degenerate. Winding is normalized via orient so the output is CCW
// regardless of the corner order the clipper produced.
func emitTriangle(out []Triangle, a, b, c *tnode) []Triangle {
	p, q, r := a.pt, b.pt, c.pt
	o := orient(p, q, r)
	if o == 0 {
		return out
	}
	if o < 0 {
		q, r = r, q
	}
	return append(out, Triangle{p, q, r})
}

// nodeArea is the signed area term earcut works in: positive when p,q,r turn
// clockwise, negative counter-clockwise (the opposite sign of orient).
func nodeArea(p, q, r Point) float64 {
	return (q.Y-p.Y)*(r.X-q.X) - (q.X-p.X)*(r.Y-q.Y)
}

// pointInTri reports whether p lies in the clockwise triangle a,b,c, boundary
// included.
func pointInTri(a, b, c, p Point) bool {
	return (c.X-p.X)*(a.Y-p.Y)-(a.X-p.X)*(c.Y-p.Y) >= 0 &&
		(a.X-p.X)*(b.Y-p.Y)-(b.X-p.X)*(a.Y-p.Y) >= 0 &&
		(b.X-p.X)*(c.Y-p.Y)-(c.X-p.X)*(b.Y-p.Y) >= 0
}

func sign(v float64) int {
	if v > 0 {
		return 1
	}
	if v < 0 {
		return -1
	}
	return 0
}
