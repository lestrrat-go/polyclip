package clip

import "github.com/lestrrat-go/polyclip/fixed"

// ZCrossing records one genuine edge crossing — a [ProperCross] where two
// segments meet strictly inside the sweep beam, producing a vertex that is not
// present in either input. E1Bot/E1Top and E2Bot/E2Top are the lower and upper
// endpoints of the two crossing segments (canonical Bot/Top order); P is the
// crossing point that appears in the output ring.
//
// Crossings are recorded only when the sweep is run via [SweepFillZ]; the
// boolean ops use them to assign Z coordinates to crossing vertices. They never
// affect X/Y geometry, so a sweep that does not record them is bit-for-bit
// identical to one that does.
type ZCrossing struct {
	E1Bot, E1Top fixed.Point
	E2Bot, E2Top fixed.Point
	P            fixed.Point
}

// SweepFillZ is [SweepFill] that additionally records every genuine edge
// crossing in [SweepResult.Crossings]. It is used by the Z-coordinate path: the
// caller maps each crossing through a user callback to a Z value for the new
// vertex. The geometry (rings, X/Y) is identical to [SweepFill]; only the extra
// recording differs, so enabling it never perturbs the result.
func SweepFillZ(segs []Segment, op Operation, fill FillRule) *SweepResult {
	s := newSweep(segs, op)
	if s.err != nil {
		return &SweepResult{Err: s.err}
	}
	s.ael.Fill = fill
	s.ael.RecordCrossings = true
	s.run()
	return &SweepResult{Trace: s.trace, Rings: s.ael.Rings(), Err: s.err, Crossings: s.ael.Crossings()}
}
