package clip

import "github.com/lestrrat-go/polyclip/fixed"

// SplitOverlaps takes a list of segments and returns a new list where every
// pair of partially-overlapping collinear segments has been split at the
// overlap endpoints. After this transformation the only collinear pairs that
// remain are either disjoint or fully coincident (identical Bot and Top) —
// the sweep relies on this invariant.
//
// Degenerate (zero-length) segments are dropped.
//
// Complexity is O(n³) in the worst case (an O(n²) scan repeated up to O(n)
// times). The function is intended for correctness-first work; Phase 5
// replaces it with a line-bucketed implementation.
func SplitOverlaps(segs []Segment) []Segment {
	work := make([]Segment, 0, len(segs))
	for _, s := range segs {
		if !s.Degenerate() {
			work = append(work, s)
		}
	}

	for {
		i, j, p, q, found := findFirstOverlap(work)
		if !found {
			return work
		}
		newI := splitAt(work[i], p, q)
		newJ := splitAt(work[j], p, q)
		out := make([]Segment, 0, len(work)+2)
		for k, s := range work {
			switch k {
			case i:
				out = append(out, newI...)
			case j:
				out = append(out, newJ...)
			default:
				out = append(out, s)
			}
		}
		work = out
	}
}

// findFirstOverlap returns the indices of the first pair in segs that
// requires a split: a CollinearOverlap whose interval does not already
// match both segments' full extent. Fully-coincident pairs (same Bot and
// same Top on both segments) are skipped — they are left for the sweep's
// own coincident-edge handling.
func findFirstOverlap(segs []Segment) (i, j int, p, q fixed.Point, found bool) {
	for i := range segs {
		for j := i + 1; j < len(segs); j++ {
			res := Intersect(segs[i], segs[j])
			if res.Kind != CollinearOverlap {
				continue
			}
			// Skip if both segments are already exactly the overlap interval.
			fullyCoincident := segs[i].Bot == segs[j].Bot && segs[i].Top == segs[j].Top
			if fullyCoincident {
				continue
			}
			// Intersect returns P and Q in LessYX order.
			return i, j, res.P, res.Q, true
		}
	}
	return 0, 0, fixed.Point{}, fixed.Point{}, false
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
