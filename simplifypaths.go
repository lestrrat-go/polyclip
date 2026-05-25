package polyclip

// SimplifyPaths reduces the vertex count of every ring in m using Clipper2's
// perpendicular-distance path-reduction algorithm (a Douglas–Peucker variant).
// A vertex is removed when its perpendicular distance to the line through its
// retained neighbours is at most epsilon; of each adjacent pair the
// smaller-deviation vertex is removed first, so collinear and near-collinear
// runs collapse cleanly. Each ring is treated as closed.
//
// SimplifyPaths is purely geometric — it does not run the boolean engine, and
// it is distinct from both [Simplify] (which resolves self-intersection) and
// [MultiPolygon.Clean] (which merges exact-duplicate and tiny features by
// tolerance). A negative epsilon is treated as zero. Rings with fewer than four
// vertices are returned unchanged (no interior vertex can be dropped without
// degenerating the ring); a ring reduced below three vertices is dropped, and
// an [ExPolygon] whose outer ring is dropped is omitted entirely.
func SimplifyPaths(m MultiPolygon, epsilon float64) MultiPolygon {
	if epsilon < 0 {
		epsilon = 0
	}
	epsSqr := epsilon * epsilon
	out := make(MultiPolygon, 0, len(m))
	for _, ex := range m {
		outer := simplifyClosedRing(ex.Outer, epsSqr)
		if len(outer) < 3 {
			continue
		}
		simplified := ExPolygon{Outer: outer}
		for _, h := range ex.Holes {
			hole := simplifyClosedRing(h, epsSqr)
			if len(hole) < 3 {
				continue
			}
			simplified.Holes = append(simplified.Holes, hole)
		}
		out = append(out, simplified)
	}
	return out
}

// simplifyClosedRing applies Clipper2's SimplifyPath reduction to a single
// closed ring. epsSqr is the squared distance tolerance. Rings with fewer than
// four vertices are returned unchanged.
func simplifyClosedRing(ring Polygon, epsSqr float64) Polygon {
	n := len(ring)
	if n < 4 {
		return ring
	}
	high := n - 1
	flags := make([]bool, n) // flagged vertices are removed
	distSqr := make([]float64, n)
	for i := range n {
		prev := ring[(i-1+n)%n]
		next := ring[(i+1)%n]
		distSqr[i] = perpDistSqr(ring[i], prev, next)
	}
	curr := 0
	for {
		if distSqr[curr] > epsSqr {
			// curr can't be removed yet; scan forward to the next removable
			// vertex, stopping if we wrap all the way around (nothing left).
			start := curr
			for {
				curr = ringNext(curr, high, flags)
				if curr == start || distSqr[curr] <= epsSqr {
					break
				}
			}
			if curr == start {
				break
			}
		}
		prior := ringPrior(curr, high, flags)
		next := ringNext(curr, high, flags)
		if next == prior {
			break
		}
		var prior2 int
		if distSqr[next] < distSqr[curr] {
			// next deviates less — remove it instead, advancing the window.
			prior2 = prior
			prior = curr
			curr = next
			next = ringNext(next, high, flags)
		} else {
			prior2 = ringPrior(prior, high, flags)
		}
		flags[curr] = true
		curr = next
		next = ringNext(next, high, flags)
		// Recompute the deviations of the two vertices now adjacent across the
		// removal (closed ring: always defined).
		distSqr[curr] = perpDistSqr(ring[curr], ring[prior], ring[next])
		distSqr[prior] = perpDistSqr(ring[prior], ring[prior2], ring[curr])
	}
	result := make(Polygon, 0, n)
	for i := range n {
		if !flags[i] {
			result = append(result, ring[i])
		}
	}
	return result
}

// perpDistSqr returns the squared perpendicular distance from pt to the line
// through line1 and line2. It returns 0 when line1 == line2.
func perpDistSqr(pt, line1, line2 Point) float64 {
	d := line2.Sub(line1)
	if d.X == 0 && d.Y == 0 {
		return 0
	}
	cross := pt.Sub(line1).Cross(d)
	return cross * cross / (d.X*d.X + d.Y*d.Y)
}

// ringNext returns the next non-removed index after current, wrapping past
// high back to 0. At least one index is always unflagged when called.
func ringNext(current, high int, flags []bool) int {
	current++
	for current <= high && flags[current] {
		current++
	}
	if current <= high {
		return current
	}
	current = 0
	for flags[current] {
		current++
	}
	return current
}

// ringPrior returns the previous non-removed index before current, wrapping
// past 0 back to high. At least one index is always unflagged when called.
func ringPrior(current, high int, flags []bool) int {
	if current == 0 {
		current = high
	} else {
		current--
	}
	for current > 0 && flags[current] {
		current--
	}
	if !flags[current] {
		return current
	}
	current = high
	for flags[current] {
		current--
	}
	return current
}
