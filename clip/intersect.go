package clip

import (
	"math"

	"github.com/lestrrat-go/polyclip/fixed"
)

// Crossing classifies how two segments meet.
type Crossing int

const (
	// NoCrossing — the segments do not share any point.
	NoCrossing Crossing = iota
	// ProperCross — the open interiors of both segments meet at one point.
	ProperCross
	// Touch — the segments meet at exactly one point and at least one of
	// those points is an endpoint of at least one segment (T-junction or
	// shared corner). [IntersectResult.P] holds the point.
	Touch
	// CollinearOverlap — the segments lie on the same line and share an
	// interval. [IntersectResult.P] and [IntersectResult.Q] hold the two
	// endpoints of that interval, ordered by [LessYX].
	CollinearOverlap
)

// IntersectResult is the outcome of [Intersect].
type IntersectResult struct {
	Kind Crossing
	P    fixed.Point
	Q    fixed.Point
}

// Intersect computes the intersection of two canonical [Segment]s. It uses
// exact integer orientation predicates from [fixed.Orient2D] to classify the
// configuration; the actual point of a proper crossing is computed in
// float64 and snapped to the integer grid, per DESIGN.md §5.2.
func Intersect(a, b Segment) IntersectResult {
	o1 := fixed.Orient2D(a.Bot, a.Top, b.Bot)
	o2 := fixed.Orient2D(a.Bot, a.Top, b.Top)
	o3 := fixed.Orient2D(b.Bot, b.Top, a.Bot)
	o4 := fixed.Orient2D(b.Bot, b.Top, a.Top)

	if o1 == 0 && o2 == 0 && o3 == 0 && o4 == 0 {
		return collinearOverlap(a, b)
	}

	// Endpoint touches: a zero orientation plus a bbox check.
	if o1 == 0 && onCollinearSegment(a, b.Bot) {
		return IntersectResult{Kind: Touch, P: b.Bot}
	}
	if o2 == 0 && onCollinearSegment(a, b.Top) {
		return IntersectResult{Kind: Touch, P: b.Top}
	}
	if o3 == 0 && onCollinearSegment(b, a.Bot) {
		return IntersectResult{Kind: Touch, P: a.Bot}
	}
	if o4 == 0 && onCollinearSegment(b, a.Top) {
		return IntersectResult{Kind: Touch, P: a.Top}
	}

	// Proper crossing: every endpoint is strictly on its own side and the
	// two sides differ.
	if o1 != 0 && o2 != 0 && o3 != 0 && o4 != 0 &&
		(o1 < 0) != (o2 < 0) && (o3 < 0) != (o4 < 0) {
		return IntersectResult{Kind: ProperCross, P: properIntersection(a, b)}
	}

	return IntersectResult{Kind: NoCrossing}
}

// onCollinearSegment reports whether p lies within the bounding rectangle of
// s. It is meaningful only when p has already been verified to be collinear
// with s via [fixed.Orient2D]; under that precondition, the bbox check is
// equivalent to "p lies on the closed segment s".
func onCollinearSegment(s Segment, p fixed.Point) bool {
	xlo, xhi := s.Bot.X, s.Top.X
	if xlo > xhi {
		xlo, xhi = xhi, xlo
	}
	ylo, yhi := s.Bot.Y, s.Top.Y
	if ylo > yhi {
		ylo, yhi = yhi, ylo
	}
	return p.X >= xlo && p.X <= xhi && p.Y >= ylo && p.Y <= yhi
}

// properIntersection computes the proper crossing point of a and b. The
// caller has verified that the segments cross strictly, so the determinant
// denominator is non-zero. The result is rounded to the integer grid.
func properIntersection(a, b Segment) fixed.Point {
	// Order the two segments deterministically so the rounded crossing point
	// does not depend on the caller's argument order. The point is computed by
	// parametrising along a, and float rounding of a+t*dir differs from
	// b+u*dir for the same geometric crossing. doIntersections can compute the
	// same crossing in two adjacent beams with the segments in swapped AEL
	// order (the crossing itself swaps them); an order-dependent result lets the
	// second value land one unit past the beam boundary, escaping the
	// already-handled guard and dispatching the crossing twice (DESIGN.md §12.11).
	if segCanonLess(b, a) {
		a, b = b, a
	}
	ax := float64(int64(a.Top.X) - int64(a.Bot.X))
	ay := float64(int64(a.Top.Y) - int64(a.Bot.Y))
	bx := float64(int64(b.Top.X) - int64(b.Bot.X))
	by := float64(int64(b.Top.Y) - int64(b.Bot.Y))
	denom := ax*by - ay*bx

	nx := float64(int64(b.Bot.X) - int64(a.Bot.X))
	ny := float64(int64(b.Bot.Y) - int64(a.Bot.Y))
	t := (nx*by - ny*bx) / denom

	return fixed.Point{
		X: fixed.Coord(math.Round(float64(a.Bot.X) + t*ax)),
		Y: fixed.Coord(math.Round(float64(a.Bot.Y) + t*ay)),
	}
}

// collinearOverlap is called when a and b lie on a common line. It returns
// NoCrossing, Touch, or CollinearOverlap depending on the overlap interval.
func collinearOverlap(a, b Segment) IntersectResult {
	// All four endpoints are collinear; sort them along the segment direction
	// to find the overlap. Horizontal segments share Y and need projection on
	// X; the general case projects on Y.
	if a.Horizontal() && b.Horizontal() {
		xlo := maxCoord(minCoord(a.Bot.X, a.Top.X), minCoord(b.Bot.X, b.Top.X))
		xhi := minCoord(maxCoord(a.Bot.X, a.Top.X), maxCoord(b.Bot.X, b.Top.X))
		if xlo > xhi {
			return IntersectResult{Kind: NoCrossing}
		}
		y := a.Bot.Y
		if xlo == xhi {
			return IntersectResult{Kind: Touch, P: fixed.Point{X: xlo, Y: y}}
		}
		return IntersectResult{
			Kind: CollinearOverlap,
			P:    fixed.Point{X: xlo, Y: y},
			Q:    fixed.Point{X: xhi, Y: y},
		}
	}

	ylo := maxCoord(a.Bot.Y, b.Bot.Y)
	yhi := minCoord(a.Top.Y, b.Top.Y)
	if ylo > yhi {
		return IntersectResult{Kind: NoCrossing}
	}
	// The overlap endpoints are always exact input endpoints (the inner two of
	// the four collinear endpoints), so take their coordinates directly. Using
	// xAtY here re-projects onto the line and ROUNDS, which can place the split
	// point a few fixed-point units off the shared line — manufacturing a
	// spurious tiny horizontal sliver that SplitOverlaps can never resolve,
	// looping forever and growing the segment slice without bound.
	p := a.Bot // endpoint achieving ylo (the higher of the two Bot.Y)
	if b.Bot.Y > a.Bot.Y {
		p = b.Bot
	}
	if ylo == yhi {
		return IntersectResult{Kind: Touch, P: p}
	}
	q := a.Top // endpoint achieving yhi (the lower of the two Top.Y)
	if b.Top.Y < a.Top.Y {
		q = b.Top
	}
	return IntersectResult{Kind: CollinearOverlap, P: p, Q: q}
}

// segCanonLess orders two segments by their (Bot, Top) endpoints lexically.
// Used to canonicalise the argument order of [properIntersection] so its
// rounded result is independent of caller order.
func segCanonLess(a, b Segment) bool {
	if a.Bot.X != b.Bot.X {
		return a.Bot.X < b.Bot.X
	}
	if a.Bot.Y != b.Bot.Y {
		return a.Bot.Y < b.Bot.Y
	}
	if a.Top.X != b.Top.X {
		return a.Top.X < b.Top.X
	}
	return a.Top.Y < b.Top.Y
}

func minCoord(a, b fixed.Coord) fixed.Coord {
	if a < b {
		return a
	}
	return b
}

func maxCoord(a, b fixed.Coord) fixed.Coord {
	if a > b {
		return a
	}
	return b
}
