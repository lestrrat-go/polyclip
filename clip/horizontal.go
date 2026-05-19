package clip

import (
	"fmt"

	"github.com/lestrrat-go/polyclip/fixed"
)

// HorizClass tags a horizontal segment by its ring context, which determines
// when in the scanline sweep the horizontal must be processed. See DESIGN.md
// §11.8 / §12.6.
type HorizClass uint8

const (
	// HorizClassUnknown is the zero value; valid horizontals are classified
	// to one of the other values by [ClassifyHorizontals].
	HorizClassUnknown HorizClass = iota
	// HorizClassMin marks a horizontal whose two ring-adjacent edges both
	// ascend from its endpoints — the horizontal forms the *bottom* of a
	// polygon. Processed via EventHoriz after both adjacent Bot events.
	HorizClassMin
	// HorizClassMax marks a horizontal whose two ring-adjacent edges both
	// descend into its endpoints — the horizontal forms the *top* of a
	// polygon. Processed via EventHorizMaxOpen before both adjacent Top
	// events so the verticals are still in the AEL when the ring closes.
	HorizClassMax
	// HorizClassMid marks a horizontal that connects an ascending edge to
	// a descending edge (or vice versa) — a step in a staircase. Not yet
	// supported by the sweep; [ClassifyHorizontals] returns an error if
	// any input horizontal falls into this class.
	HorizClassMid
)

// HorizInfo annotates one horizontal segment with the context the sweep needs:
// its ring-flavor and pointers to the two adjacent non-horizontal segments.
//
// LeftAdj is the segment whose endpoint coincides with the horizontal's
// (lex-smaller) Bot; RightAdj coincides with the horizontal's Top. For a
// HorizClassMin horizontal, both adjacents start (Bot end of their canonical
// form) at the horizontal's Y. For HorizClassMax, both adjacents terminate
// (Top end) at the horizontal's Y.
type HorizInfo struct {
	Class    HorizClass
	LeftAdj  *Segment
	RightAdj *Segment
}

// ErrUnsupportedHorizontal is returned by [ClassifyHorizontals] when a
// horizontal segment cannot be matched to a HorizClassMin or HorizClassMax
// context — typically a mid-bound horizontal in a staircase. Phase 2 first
// cut handles only axis-aligned local-min and local-max horizontals; the
// mid-bound case is deferred to a later increment.
var ErrUnsupportedHorizontal = fmt.Errorf("clip: horizontal segment is neither a local minimum nor a local maximum of its ring (mid-bound horizontal not yet supported)")

// ClassifyHorizontals scans segs and returns a map from each horizontal
// segment to its ring-context [HorizInfo]. The segs slice is treated as the
// flat output of preprocess: every segment is a directed edge in canonical
// (Bot, Top) form with a Source tag and a Reversed flag indicating whether
// the canonical direction matches the input ring's traversal.
//
// The function reconstructs ring adjacency from the input direction —
// segment B follows segment A in the same input ring iff A.End() == B.Start().
// For non-self-intersecting input this is unambiguous; if multiple segments
// start (or end) at the same vertex, ClassifyHorizontals picks one and the
// result may be wrong. SplitOverlaps' invariant (no partial collinear
// overlaps remain) plus well-formed input rings is sufficient for the
// reconstruction to be unambiguous.
//
// Returns [ErrUnsupportedHorizontal] if any horizontal cannot be classified
// as HorizClassMin or HorizClassMax.
func ClassifyHorizontals(segs []Segment) (map[*Segment]*HorizInfo, error) {
	result := make(map[*Segment]*HorizInfo)
	if len(segs) == 0 {
		return result, nil
	}
	byStart := make(map[fixed.Point]*Segment, len(segs))
	byEnd := make(map[fixed.Point]*Segment, len(segs))
	for i := range segs {
		s := &segs[i]
		if s.Degenerate() {
			continue
		}
		byStart[s.Start()] = s
		byEnd[s.End()] = s
	}

	for i := range segs {
		s := &segs[i]
		if !s.Horizontal() || s.Degenerate() {
			continue
		}
		info, err := classifyOne(s, byStart, byEnd)
		if err != nil {
			return nil, err
		}
		result[s] = info
	}
	return result, nil
}

func classifyOne(h *Segment, byStart, byEnd map[fixed.Point]*Segment) (*HorizInfo, error) {
	prev := byEnd[h.Start()]
	next := byStart[h.End()]
	if prev == nil || next == nil {
		// A horizontal with no ring-adjacent edges is not part of a closed
		// polygon — typically test fixtures, or an input bug. Tag it as
		// Unknown so the sweep treats it as a trace-only no-op rather than
		// failing the entire operation.
		return &HorizInfo{Class: HorizClassUnknown}, nil
	}
	prevDir := yDirection(prev)
	nextDir := yDirection(next)

	// For a local-min horizontal, the *input-direction* predecessor descends
	// to h's start vertex and the successor ascends from h's end vertex.
	// For a local-max horizontal, predecessor ascends and successor descends.
	class := HorizClassMid
	switch {
	case prevDir == yDown && nextDir == yUp:
		class = HorizClassMin
	case prevDir == yUp && nextDir == yDown:
		class = HorizClassMax
	}
	if class == HorizClassMid {
		return nil, fmt.Errorf("%w: horizontal %v→%v (prev y-dir=%v, next y-dir=%v)", ErrUnsupportedHorizontal, h.Start(), h.End(), prevDir, nextDir)
	}

	// Pair adjacents with h's canonical Bot/Top endpoints.
	leftAdj, rightAdj := adjacentsByX(h, prev, next)
	return &HorizInfo{Class: class, LeftAdj: leftAdj, RightAdj: rightAdj}, nil
}

// yDir is a Y-direction tag for the canonical direction of a non-horizontal
// segment relative to the *input* direction. yUp means the input-direction
// edge goes from lower Y to higher Y; yDown means higher Y to lower Y.
type yDir uint8

const (
	yFlat yDir = iota
	yUp
	yDown
)

func (d yDir) String() string {
	switch d {
	case yUp:
		return "up"
	case yDown:
		return "down"
	case yFlat:
		return "flat"
	}
	return "?"
}

func yDirection(s *Segment) yDir {
	if s.Horizontal() {
		return yFlat
	}
	if s.Reversed {
		return yDown
	}
	return yUp
}

// adjacentsByX pairs prev and next with h's canonical (lex-smaller) Bot and
// (lex-larger) Top endpoints. For a horizontal with input direction Bot→Top
// (Reversed=false), prev shares h.Bot and next shares h.Top; for input
// direction Top→Bot (Reversed=true), the pairing is swapped.
func adjacentsByX(h, prev, next *Segment) (left, right *Segment) {
	if h.Start() == h.Bot {
		return prev, next
	}
	return next, prev
}
