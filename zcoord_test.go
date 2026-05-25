package polyclip

import "testing"

// constZ assigns a fixed Z to every crossing vertex.
type constZ float64

func (c constZ) AssignZ(_, _, _, _, _ Point) float64 { return float64(c) }

// coordZ encodes the crossing point into Z as X*100+Y, so a test can assert
// both that the callback fired and that it received the right crossing point.
type coordZ struct{}

func (coordZ) AssignZ(_, _, _, _ Point, crossing Point) float64 {
	return crossing.X*100 + crossing.Y
}

// recordZ captures every AssignZ call for inspection.
type recordZ struct {
	calls [][5]Point
}

func (r *recordZ) AssignZ(e1bot, e1top, e2bot, e2top, crossing Point) float64 {
	r.calls = append(r.calls, [5]Point{e1bot, e1top, e2bot, e2top, crossing})
	return 0
}

// zAt returns the Z of the result vertex at (x,y), or NaN-substitute via found.
func zAt(m MultiPolygon, x, y float64) (float64, bool) {
	for _, ex := range m {
		for _, p := range ex.Outer {
			if p.X == x && p.Y == y {
				return p.Z, true
			}
		}
		for _, h := range ex.Holes {
			for _, p := range h {
				if p.X == x && p.Y == y {
					return p.Z, true
				}
			}
		}
	}
	return 0, false
}

func TestBuilderZCrossingAndInputPreserved(t *testing.T) {
	// A = [0,0]-[10,10] with Z=1 on every vertex; B = [5,5]-[15,15] with Z=2.
	// Intersect = [5,5]-[10,10]. Its corners:
	//   (5,5)   B's vertex inside A   -> input Z 2
	//   (10,10) A's vertex inside B   -> input Z 1
	//   (10,5)  crossing A-right×B-bottom -> assigned 10*100+5 = 1005
	//   (5,10)  crossing A-top×B-left     -> assigned 5*100+10 = 510
	a := MultiPolygon{{Outer: Polygon{
		{X: 0, Y: 0, Z: 1}, {X: 10, Y: 0, Z: 1}, {X: 10, Y: 10, Z: 1}, {X: 0, Y: 10, Z: 1},
	}}}
	b := MultiPolygon{{Outer: Polygon{
		{X: 5, Y: 5, Z: 2}, {X: 15, Y: 5, Z: 2}, {X: 15, Y: 15, Z: 2}, {X: 5, Y: 15, Z: 2},
	}}}

	res, err := NewBuilder().AddSubject(a).AddClip(b).SetZAssigner(coordZ{}).Execute(OpIntersect)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := map[[2]float64]float64{
		{5, 5}:   2,
		{10, 10}: 1,
		{10, 5}:  1005,
		{5, 10}:  510,
	}
	for xy, wz := range want {
		z, ok := zAt(res.Closed, xy[0], xy[1])
		if !ok {
			t.Errorf("vertex (%g,%g) missing from result", xy[0], xy[1])
			continue
		}
		if z != wz {
			t.Errorf("Z at (%g,%g) = %g, want %g", xy[0], xy[1], z, wz)
		}
	}
}

func TestBuilderZDisabledIsZero(t *testing.T) {
	// Without an assigner, output Z is zero even when inputs carry Z (the engine
	// ignores Z; the free functions never touch it).
	a := MultiPolygon{{Outer: Polygon{
		{X: 0, Y: 0, Z: 1}, {X: 10, Y: 0, Z: 1}, {X: 10, Y: 10, Z: 1}, {X: 0, Y: 10, Z: 1},
	}}}
	b := MultiPolygon{{Outer: Polygon{
		{X: 5, Y: 5, Z: 2}, {X: 15, Y: 5, Z: 2}, {X: 15, Y: 15, Z: 2}, {X: 5, Y: 15, Z: 2},
	}}}
	res, err := NewBuilder().AddSubject(a).AddClip(b).Execute(OpIntersect)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, ex := range res.Closed {
		for _, p := range ex.Outer {
			if p.Z != 0 {
				t.Errorf("Z = %g at (%g,%g), want 0 (tracking disabled)", p.Z, p.X, p.Y)
			}
		}
	}
}

func TestBuilderZConstantOnAllCrossings(t *testing.T) {
	// Every corner of the intersection rectangle that is a genuine crossing gets
	// the constant; the two corners that are input vertices keep Z 0 (inputs here
	// carry no Z).
	a := MultiPolygon{{Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}}}
	b := MultiPolygon{{Outer: Polygon{{X: 5, Y: 5}, {X: 15, Y: 5}, {X: 15, Y: 15}, {X: 5, Y: 15}}}}
	res, err := NewBuilder().AddSubject(a).AddClip(b).SetZAssigner(constZ(7)).Execute(OpIntersect)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, xy := range [][2]float64{{10, 5}, {5, 10}} {
		z, ok := zAt(res.Closed, xy[0], xy[1])
		if !ok || z != 7 {
			t.Errorf("crossing (%g,%g): Z=%g ok=%v, want 7", xy[0], xy[1], z, ok)
		}
	}
}

func TestBuilderZAssignerEndpoints(t *testing.T) {
	// The assigner is called once per genuine crossing with that crossing's
	// point; the two crossings of the overlapping squares are (10,5) and (5,10).
	a := MultiPolygon{{Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}}}
	b := MultiPolygon{{Outer: Polygon{{X: 5, Y: 5}, {X: 15, Y: 5}, {X: 15, Y: 15}, {X: 5, Y: 15}}}}
	rec := &recordZ{}
	if _, err := NewBuilder().AddSubject(a).AddClip(b).SetZAssigner(rec).Execute(OpIntersect); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(rec.calls) != 2 {
		t.Fatalf("AssignZ called %d times, want 2", len(rec.calls))
	}
	seen := map[[2]float64]bool{}
	for _, c := range rec.calls {
		cr := c[4]
		seen[[2]float64{cr.X, cr.Y}] = true
	}
	for _, xy := range [][2]float64{{10, 5}, {5, 10}} {
		if !seen[xy] {
			t.Errorf("no AssignZ call for crossing (%g,%g)", xy[0], xy[1])
		}
	}
}

func TestBuilderZXorComposition(t *testing.T) {
	// Xor is computed by composition (Union, Intersect, Difference). Z tracking
	// must still flow through: the same crossing points appear and get assigned.
	a := MultiPolygon{{Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}}}
	b := MultiPolygon{{Outer: Polygon{{X: 5, Y: 5}, {X: 15, Y: 5}, {X: 15, Y: 15}, {X: 5, Y: 15}}}}
	res, err := NewBuilder().AddSubject(a).AddClip(b).SetZAssigner(constZ(9)).Execute(OpXor)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, xy := range [][2]float64{{10, 5}, {5, 10}} {
		z, ok := zAt(res.Closed, xy[0], xy[1])
		if !ok || z != 9 {
			t.Errorf("Xor crossing (%g,%g): Z=%g ok=%v, want 9", xy[0], xy[1], z, ok)
		}
	}
}
