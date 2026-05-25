package polyclip

import (
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-go/polyclip/clip"
)

// skyline builds a simple CCW rectilinear "histogram" polygon: m unit-width
// columns sitting on y=0 with the given heights. Rich in mid-bound horizontal
// edges (every monotone run of column tops is a staircase step that
// ClassifyHorizontals would reject as HorizClassMid).
func skyline(x0, y0 int, heights []int) Polygon {
	m := len(heights)
	ring := Polygon{}
	// bottom-left up the left wall, then the top profile left-to-right is built
	// by walking columns; assemble CCW: bottom edge first.
	ring = append(ring, Point{X: float64(x0), Y: float64(y0)})
	ring = append(ring, Point{X: float64(x0 + m), Y: float64(y0)})
	// right wall up to last column top
	cur := heights[m-1]
	ring = append(ring, Point{X: float64(x0 + m), Y: float64(y0 + cur)})
	// walk columns right-to-left along the top profile
	for i := m - 1; i >= 0; i-- {
		h := heights[i]
		if h != cur {
			// vertical step at x = x0+i+1
			ring = append(ring, Point{X: float64(x0 + i + 1), Y: float64(y0 + h)})
			cur = h
		}
		// horizontal top of column i to its left boundary x=x0+i
		ring = append(ring, Point{X: float64(x0 + i), Y: float64(y0 + h)})
	}
	// down the left wall back to start (the last point added is (x0, heights[0]))
	// closing edge to (x0,y0) is implicit.
	return ring
}

func randSkyline(rng *rand.Rand, x0, y0, m, maxH int) Polygon {
	heights := make([]int, m)
	for i := range heights {
		heights[i] = 1 + rng.Intn(maxH)
	}
	return skyline(x0, y0, heights)
}

// TestHorizontalFallbackReachability is the DESIGN §7.5 reachability
// characterization. It hammers the engine with axis-aligned skyline polygons
// (dense in mid-bound horizontals) combined under all four boolean ops plus
// Simplify, recording how often the BuildLocalMinima bound-model pre-pass
// falls back to the legacy ClassifyHorizontals dispatch and how often that
// path surfaces ErrHorizontalNotSupported on a VALIDATED input.
//
// It asserts no input hangs the engine (the per-op watchdog t.Fatalf's on a
// hang — see the processHorzJoins fix and TestHorizJoinHangRepro) AND that the
// algebraic identities hold on every interacting pair (idFails == 0). The
// reported fellBack / horizErr counts are the §7.5 finding (the fallback IS
// heavily reachable); idFails was the §7.6 coincident/cross-source mis-resolution
// (now fixed — asserted as a regression guard).
func TestHorizontalFallbackReachability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping reachability sweep in -short")
	}
	clip.SetFallbackTraceEnabled(true)
	defer func() {
		clip.SetFallbackTraceEnabled(false)
		clip.ClearFallbackTrace()
	}()

	var (
		pairs       int
		fellBack    int
		horizErr    int
		otherErr    int
		idFails     int
		firstHoriz  string
		firstFellBk string
		firstIdFail string
	)

	// runOp runs fn under a 2s watchdog, failing the test (with the input) on a
	// hang. Returns the result area and whether the op errored.
	runOp := func(opName string, fn func(a, b MultiPolygon) (MultiPolygon, error), a, b MultiPolygon) (float64, bool) {
		clip.ClearFallbackTrace()
		type res struct {
			m MultiPolygon
			e error
		}
		done := make(chan res, 1)
		go func() {
			m, e := fn(a, b)
			done <- res{m, e}
		}()
		var r res
		select {
		case r = <-done:
		case <-time.After(2 * time.Second):
			t.Fatalf("HANG in %s on A=%s B=%s", opName, mpStr(a), mpStr(b))
		}
		pairs++
		if tr := clip.FallbackTrace(); len(tr) > 0 {
			fellBack++
			if firstFellBk == "" {
				firstFellBk = tr[0]
			}
		}
		if r.e != nil {
			if errors.Is(r.e, ErrHorizontalNotSupported) {
				horizErr++
				if firstHoriz == "" {
					firstHoriz = r.e.Error() + "  A=" + mpStr(a) + " B=" + mpStr(b)
				}
			} else {
				otherErr++
			}
			return 0, false
		}
		return r.m.Area(), true
	}

	for seed := range 40 {
		rng := rand.New(rand.NewSource(int64(seed)*7919 + 3))
		for range 400 {
			m := 2 + rng.Intn(6)
			a := MultiPolygon{ExPolygon{Outer: randSkyline(rng, 0, 0, m, 6)}}
			// B overlaps A's domain so preprocessing creates shared vertices /
			// collinear overlaps; bias toward shared coordinates by reusing the
			// same lattice and frequently the same origin.
			bx, by := rng.Intn(m+1), rng.Intn(7)-3
			b := MultiPolygon{ExPolygon{Outer: randSkyline(rng, bx, by, 1+rng.Intn(6), 6)}}
			if len(a.Validate()) != 0 || len(b.Validate()) != 0 {
				continue
			}
			if !a.BoundingBox().Intersects(b.BoundingBox()) {
				continue
			}
			uA, uok := runOp("Union", Union, a, b)
			iA, iok := runOp("Intersect", Intersect, a, b)
			dA, dok := runOp("Difference", Difference, a, b)
			xA, xok := runOp("Xor", Xor, a, b)
			// Algebraic identities are exact on integer-lattice inputs: a
			// violation is a real region drop / double-count, not snap noise.
			if uok && iok && dok && xok {
				aA, bA := a.Area(), b.Area()
				bad := abs(uA-(aA+bA-iA)) > 1e-6 || abs(dA-(aA-iA)) > 1e-6 || abs(xA-(uA-iA)) > 1e-6
				if bad {
					idFails++
					if firstIdFail == "" {
						firstIdFail = "A=" + mpStr(a) + " B=" + mpStr(b) +
							" U=" + ftoaf(uA) + " I=" + ftoaf(iA) + " D=" + ftoaf(dA) + " X=" + ftoaf(xA) +
							" Aa=" + ftoaf(aA) + " Ba=" + ftoaf(bA)
					}
				}
			}
			// Simplify on both as a single self-overlapping source.
			both := MultiPolygon{a[0], b[0]}
			clip.ClearFallbackTrace()
			sdone := make(chan error, 1)
			go func() { _, e := Simplify(both); sdone <- e }()
			select {
			case serr := <-sdone:
				pairs++
				if tr := clip.FallbackTrace(); len(tr) > 0 {
					fellBack++
					if firstFellBk == "" {
						firstFellBk = tr[0]
					}
				}
				if serr != nil {
					if errors.Is(serr, ErrHorizontalNotSupported) {
						horizErr++
					} else {
						otherErr++
					}
				}
			case <-time.After(2 * time.Second):
				t.Fatalf("HANG in Simplify on %s", mpStr(both))
			}
		}
	}

	t.Logf("REACHABILITY: ops=%d fellBack=%d horizErr=%d otherErr=%d idFails=%d", pairs, fellBack, horizErr, otherErr, idFails)
	if firstFellBk != "" {
		t.Logf("first fallback: %s", firstFellBk)
	}
	if firstHoriz != "" {
		t.Logf("first ErrHorizontalNotSupported: %s", firstHoriz)
	}
	// §7.6 is now FIXED: the algebraic identities are exact on every interacting
	// axis-aligned pair. Assert it — a regression means a coincident /
	// cross-source confluence is mis-resolving again (DESIGN §7.6).
	if idFails != 0 {
		t.Errorf("§7.6 regression: %d identity violations (want 0); first: %s", idFails, firstIdFail)
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func ftoaf(f float64) string { return fmt.Sprintf("%g", f) }

func mpStr(m MultiPolygon) string {
	var b strings.Builder
	for _, ex := range m {
		b.WriteByte('[')
		for _, p := range ex.Outer {
			fmt.Fprintf(&b, "(%g,%g)", p.X, p.Y)
		}
		b.WriteByte(']')
	}
	return b.String()
}
