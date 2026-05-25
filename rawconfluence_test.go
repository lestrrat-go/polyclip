package polyclip

import (
	"math/rand"
	"testing"

	"github.com/lestrrat-go/polyclip/clip"
	"github.com/lestrrat-go/polyclip/fixed"
)

// rawOp runs the engine pipeline with NO result-level masking: no subset filter,
// no per-piece Difference decomposition, no Xor-by-composition (unlike the public
// ops). It exposes the true IN-SWEEP correctness so the §7.6/§7.7 confluence
// rework can be measured directly — the public ops report zero identity
// violations only because those masks hide the residual sweep-level over-trace.
func rawOp(a, b MultiPolygon, op clip.Operation) (MultiPolygon, error) {
	bbox := a.BoundingBox().Union(b.BoundingBox())
	scale := fixed.ScaleFromBBox(bbox.Min.X, bbox.Min.Y, bbox.Max.X, bbox.Max.Y)

	segs := collectSegments(a, clip.Subject, scale, nil)
	segs = append(segs, collectSegments(b, clip.Clip, scale, nil)...)
	segs = clip.SplitOverlaps(segs)
	segs = clip.SplitTJunctions(segs)
	segs = clip.DedupCoincidentEdges(segs)
	sw := clip.Sweep(segs, op)
	if sw.Err != nil {
		return nil, sw.Err
	}
	return assembleResult(sw.Rings, scale, nil), nil
}

// countRawIdFails counts UNMASKED algebraic-identity violations over the skyline
// corpus (axis-aligned shared-vertex inputs, the §7.6 stress set).
func countRawIdFails() (int, int, int, int) {
	tot, nU, nD, nX := 0, 0, 0, 0
	for seed := range 40 {
		rng := rand.New(rand.NewSource(int64(seed)*7919 + 3))
		for range 400 {
			m := 2 + rng.Intn(6)
			a := MultiPolygon{ExPolygon{Outer: randSkyline(rng, 0, 0, m, 6)}}
			bx, by := rng.Intn(m+1), rng.Intn(7)-3
			b := MultiPolygon{ExPolygon{Outer: randSkyline(rng, bx, by, 1+rng.Intn(6), 6)}}
			if len(a.Validate()) != 0 || len(b.Validate()) != 0 {
				continue
			}
			if !a.BoundingBox().Intersects(b.BoundingBox()) {
				continue
			}
			u, ue := rawOp(a, b, clip.OpUnion)
			i, ie := rawOp(a, b, clip.OpIntersect)
			d, de := rawOp(a, b, clip.OpDifference)
			x, xe := rawOp(a, b, clip.OpXor)
			if ue != nil || ie != nil || de != nil || xe != nil {
				continue
			}
			uA, iA, dA, xA := u.Area(), i.Area(), d.Area(), x.Area()
			aA, bA := a.Area(), b.Area()
			ub := abs(uA-(aA+bA-iA)) > 1e-6
			db := abs(dA-(aA-iA)) > 1e-6
			xb := abs(xA-(uA-iA)) > 1e-6
			if ub || db || xb {
				tot++
				if ub {
					nU++
				}
				if db {
					nD++
				}
				if xb {
					nX++
				}
			}
		}
	}
	return tot, nU, nD, nX
}

// TestRawInSweepIdFailRatchet locks in the in-sweep confluence-rework progress.
// The raw (unmasked) sweep still over-traces a residual class of coincident
// cross-source confluences (§7.6/§7.7); the public ops mask it (subset filter,
// Xor composition). This ratchets the residual so the rework can only shrink it.
// Current residual: 6 direct-OpXor cases. The Intersect-over-count cluster was
// closed by the confluence force-close rule (terminating contributing edge +
// hole-aware membership); one further Xor case closed by enabling the
// horizontal-join reconnection pass for Xor. Lower the bound as the rework
// progresses; never raise it.
func TestRawInSweepIdFailRatchet(t *testing.T) {
	const ratchet = 6
	tot, nU, nD, nX := countRawIdFails()
	t.Logf("raw in-sweep idfails: total=%d U=%d D=%d X=%d (ratchet=%d)", tot, nU, nD, nX, ratchet)
	if tot > ratchet {
		t.Errorf("raw in-sweep idfails regressed: got %d, ratchet %d", tot, ratchet)
	}
}

// TestRawIntersectOuterCornerCloses guards the §7.6 confluence force-close rule.
// At (5,2) subject A's top-right corner maxes where clip B's wall continues up;
// the coincident A-top/B-edge horizontal must close the Intersect ring (B's
// terminating-or-continuing wall must not over-trace into a spurious lobe
// outside A). Raw (unmasked) Intersect must equal the true area, not 6.
func TestRawIntersectOuterCornerCloses(t *testing.T) {
	a := MultiPolygon{ExPolygon{Outer: []Point{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 2}, {X: 4, Y: 2}, {X: 3, Y: 2}, {X: 2, Y: 2}, {X: 2, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 4}, {X: 0, Y: 4},
	}}}
	b := MultiPolygon{ExPolygon{Outer: []Point{
		{X: 2, Y: -1}, {X: 7, Y: -1}, {X: 7, Y: 5}, {X: 6, Y: 5}, {X: 6, Y: 3}, {X: 5, Y: 3}, {X: 5, Y: 2}, {X: 4, Y: 2}, {X: 4, Y: 1}, {X: 3, Y: 1}, {X: 3, Y: 5}, {X: 2, Y: 5},
	}}}
	got, err := rawOp(a, b, clip.OpIntersect)
	if err != nil {
		t.Fatal(err)
	}
	if want := 5.0; abs(got.Area()-want) > 1e-6 {
		t.Errorf("raw Intersect area = %v, want %v (spurious lobe not closed)", got.Area(), want)
	}
}

// TestRawXorCoincidentReconnect guards the §7.6 raw-Xor horizontal-join fix.
// A and B share several collinear boundary edges; the direct OpXor sweep used
// to over-trace the coincident horizontal overlaps (standard maximum handling
// alone), inflating the symmetric difference. Enabling the horz-join
// reconnection pass for Xor (as U/I/D use) closes them. True Xor = U−I = 5.
func TestRawXorCoincidentReconnect(t *testing.T) {
	a := MultiPolygon{ExPolygon{Outer: []Point{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 3}, {X: 4, Y: 3}, {X: 4, Y: 1}, {X: 3, Y: 1}, {X: 3, Y: 5}, {X: 2, Y: 5}, {X: 2, Y: 1}, {X: 1, Y: 1}, {X: 1, Y: 5}, {X: 0, Y: 5},
	}}}
	b := MultiPolygon{ExPolygon{Outer: []Point{
		{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 5}, {X: 4, Y: 5}, {X: 4, Y: 2}, {X: 3, Y: 2}, {X: 3, Y: 5}, {X: 2, Y: 5}, {X: 2, Y: 3}, {X: 1, Y: 3}, {X: 1, Y: 5}, {X: 0, Y: 5},
	}}}
	got, err := rawOp(a, b, clip.OpXor)
	if err != nil {
		t.Fatal(err)
	}
	if want := 5.0; abs(got.Area()-want) > 1e-6 {
		t.Errorf("raw Xor area = %v, want %v (coincident horizontals over-traced)", got.Area(), want)
	}
}
