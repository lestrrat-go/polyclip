package clip

import "github.com/lestrrat-go/polyclip/internal/fixed"

// Source identifies which input polygon a segment came from. The Vatti
// algorithm distinguishes the two inputs because the boolean operation's
// emission rule depends on the running winding count of each.
type Source uint8

const (
	Subject Source = iota // the "a" input to a boolean operation
	Clip                  // the "b" input
)

// Segment is a directed edge between two points on the integer grid.
//
// Segments are stored in canonical form: Bot is the endpoint with the lower
// (Y, X) ordering and Top is the higher one. A separate [Segment.Reversed]
// flag records whether this canonical direction matches the input polygon's
// own edge direction or is reversed. Storing edges canonically is convenient
// for the scanline sweep, which processes events in Y order.
type Segment struct {
	Bot      fixed.Point
	Top      fixed.Point
	Src      Source
	Reversed bool
}

// NewSegment constructs a Segment for the directed edge a→b coming from
// source src. Degenerate edges (a == b) are returned with Bot == Top and
// Reversed == false; callers should filter them out before feeding the sweep.
func NewSegment(a, b fixed.Point, src Source) Segment {
	if LessYX(a, b) {
		return Segment{Bot: a, Top: b, Src: src, Reversed: false}
	}
	if a == b {
		return Segment{Bot: a, Top: b, Src: src, Reversed: false}
	}
	return Segment{Bot: b, Top: a, Src: src, Reversed: true}
}

// Degenerate reports whether the segment has zero length on the integer grid.
func (s Segment) Degenerate() bool {
	return s.Bot == s.Top
}

// Horizontal reports whether Bot.Y == Top.Y. Horizontal segments are special
// in the scanline sweep because they span an entire event row.
func (s Segment) Horizontal() bool {
	return s.Bot.Y == s.Top.Y
}

// Start returns the segment's start point in its original (input) direction.
func (s Segment) Start() fixed.Point {
	if s.Reversed {
		return s.Top
	}
	return s.Bot
}

// End returns the segment's end point in its original (input) direction.
func (s Segment) End() fixed.Point {
	if s.Reversed {
		return s.Bot
	}
	return s.Top
}

// LessYX reports whether a sorts before b in lexicographic (Y, X) order. It
// is the canonical comparator for both segment orientation and event-queue
// ordering.
func LessYX(a, b fixed.Point) bool {
	if a.Y != b.Y {
		return a.Y < b.Y
	}
	return a.X < b.X
}
