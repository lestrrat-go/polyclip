package polyclip

import (
	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// ZAssigner computes the Z coordinate for a vertex the engine creates where two
// input edges cross. The four points are the lower and upper endpoints of the
// two crossing edges; crossing is the new vertex with X and Y set and Z zero.
// The returned value becomes that vertex's Z.
//
// Install one with [Builder.SetZAssigner] to enable Z tracking: input vertices'
// Z values are then carried through to the output, and each crossing vertex
// gets AssignZ. This mirrors Clipper2's ZCallback (DESIGN.md §7.8h). Without an
// assigner the engine ignores Z entirely (the default), so the standard path is
// unchanged and pays no cost.
type ZAssigner interface {
	AssignZ(e1bot, e1top, e2bot, e2top, crossing Point) float64
}

// zTracker carries the Z assigner and the grid-point→Z table for one run of the
// sweep path. It is nil on the default (Z-free) path; every method is a no-op
// on a nil receiver, so threading it through the engine adds no cost when Z is
// not in use.
type zTracker struct {
	za    ZAssigner
	table map[fixed.Point]float64
}

// newZTracker returns a tracker for za, or nil when za is nil (Z disabled).
func newZTracker(za ZAssigner) *zTracker {
	if za == nil {
		return nil
	}
	return &zTracker{za: za, table: make(map[fixed.Point]float64)}
}

// recordInput stores an input vertex's Z under its snapped grid point. Input
// vertices take precedence over crossings at the same grid point.
func (z *zTracker) recordInput(fp fixed.Point, zv float64) {
	if z == nil {
		return
	}
	z.table[fp] = zv
}

// applyCrossings maps each genuine sweep crossing through the assigner and
// records the result under the crossing's grid point — unless an input vertex
// already claimed it (input Z wins). The endpoints and crossing point are
// unsnapped to user units for the callback.
func (z *zTracker) applyCrossings(cs []clip.ZCrossing, scale fixed.Scale) {
	if z == nil {
		return
	}
	for _, c := range cs {
		if _, ok := z.table[c.P]; ok {
			continue
		}
		z.table[c.P] = z.za.AssignZ(
			unsnapPoint(c.E1Bot, scale), unsnapPoint(c.E1Top, scale),
			unsnapPoint(c.E2Bot, scale), unsnapPoint(c.E2Top, scale),
			unsnapPoint(c.P, scale),
		)
	}
}

// lookup returns the Z recorded for grid point fp, or 0 if none (including on a
// nil tracker).
func (z *zTracker) lookup(fp fixed.Point) float64 {
	if z == nil {
		return 0
	}
	return z.table[fp]
}

// unsnapPoint converts a grid point back to a user-unit Point (Z zero).
func unsnapPoint(fp fixed.Point, scale fixed.Scale) Point {
	x, y := scale.Unsnap(fp)
	return Point{X: x, Y: y}
}
