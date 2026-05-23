// Command differential is a randomized Monte-Carlo differential harness for
// the polyclip boolean engine — the standing tool for hunting the engine's
// degenerate-input residuals.
//
// It generates random simple-quad pairs (optionally forcing a shared
// degeneracy), runs all four boolean ops, and compares each result to a
// Monte-Carlo area oracle and to the noise-free algebraic identities
// (U=A+B-I, D=A-I, X=U-I). It REPORTS discrepancies; it asserts nothing. The
// top-ranked fails are the live bugs; near-tolerance fails are MC noise.
//
// Run all scenarios, or filter by name substring:
//
//	go run ./tools/differential
//	go run ./tools/differential degenerate
//
// All knobs are code constants in [scenarios] below — edit them to retarget;
// there are no env vars or flags. When chasing one specific bug, add throwaway
// trace hooks / package toggles in the library and delete THOSE afterwards;
// this tool stays.
//
// SIZING: keep pairs*samples <= ~5e8 per scenario so each finishes well under
// a minute. MC noise over a [0,ext] bbox is ~ext^2 * sqrt(1/samples) per op,
// so a tight tol at large ext drowns real gross fails in noise — there, prefer
// the noise-free identity checks and disable MC.
package main

import (
	"fmt"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/lestrrat-go/polyclip"
)

type genMode int

const (
	genRandom     genMode = iota // two independent random quads
	genDegenerate                // B forced to share a degeneracy with A
	genHole                      // A is a square with a hole; B touches the hole
)

type scenario struct {
	name string
	ext  int // coordinate range [0,ext]; quads use distinct lattice points

	seeds   int
	pairs   int // pairs per seed
	samples int // MC samples per pair (ignored when checkMC is false)
	tol     float64

	mode          genMode
	checkMC       bool // absolute area vs Monte-Carlo: catches region drops
	checkIdentity bool // U=A+B-I, D=A-I, X=U-I: noise-free, catches double-counts
}

// scenarios lists the configured runs. Tune in place.
var scenarios = []scenario{
	// Small coords: MC noise is tiny (~0.09 at 120k samples), so the absolute
	// MC check reliably catches region drops; tol 0.6 separates gross from noise.
	{
		name: "random-small", ext: 8,
		seeds: 6, pairs: 1000, samples: 120000, tol: 0.6,
		mode: genRandom, checkMC: true, checkIdentity: true,
	},
	// Large coords: MC noise (~2.3 at ext=40) swamps a useful tol, so rely on
	// the noise-free identities and skip MC. Cheap, so run many pairs.
	{
		name: "random-large", ext: 40,
		seeds: 8, pairs: 8000, samples: 0, tol: 1.0,
		mode: genRandom, checkMC: false, checkIdentity: true,
	},
	// Forced degeneracy: where the residuals actually live (shared vertex,
	// vertex-on-edge, collinear-overlapping edge). Moderate coords keep MC
	// noise low enough for the absolute check to catch region drops.
	{
		name: "degenerate", ext: 12,
		seeds: 6, pairs: 3000, samples: 60000, tol: 0.6,
		mode: genDegenerate, checkMC: true, checkIdentity: true,
	},
	// Holes: A is an [0,ext] square with a random CW hole; B is a quad that
	// frequently touches or crosses the hole boundary (forced-degenerate
	// against the hole ring). Exercises the hole-nesting path (interiorPoint /
	// ringInside / hole-vs-outer classification) that the quad-only scenarios
	// never reach.
	{
		name: "holes", ext: 12,
		seeds: 6, pairs: 3000, samples: 60000, tol: 0.6,
		mode: genHole, checkMC: true, checkIdentity: true,
	},
}

func main() {
	filter := ""
	if len(os.Args) > 1 {
		filter = os.Args[1]
	}
	for _, sc := range scenarios {
		if filter != "" && !strings.Contains(sc.name, filter) {
			continue
		}
		run(sc)
	}
}

type fail struct {
	op        string
	a, b      polyclip.MultiPolygon
	got, want float64
}

// genPair builds an (A, B) input pair for the scenario's mode. It returns
// ok=false when a valid pair couldn't be built (caller skips the iteration).
// The genRandom/genDegenerate rng call sequences are kept identical to the
// original inline code so those scenarios' pair streams don't shift.
func genPair(rng *rand.Rand, sc scenario) (polyclip.MultiPolygon, polyclip.MultiPolygon, bool) {
	switch sc.mode {
	case genRandom:
		a := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: randQuad(rng, sc.ext)}}
		b := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: randQuad(rng, sc.ext)}}
		return a, b, true
	case genDegenerate:
		ra := randQuad(rng, sc.ext)
		rb, ok := forceDegenerate(rng, sc.ext, ra, rng.Intn(3))
		if !ok {
			return nil, nil, false
		}
		a := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: ra}}
		b := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: rb}}
		return a, b, true
	case genHole:
		return genHolePair(rng, sc.ext)
	}
	return nil, nil, false
}

// genHolePair returns A = an [0,ext] square with a single random CW hole, and
// B = a quad that (half the time) is forced to share a degeneracy with the
// hole ring so it touches or crosses the hole boundary. Both inputs are
// Validate()-checked, so only well-formed MultiPolygons reach the engine.
func genHolePair(rng *rand.Rand, ext int) (polyclip.MultiPolygon, polyclip.MultiPolygon, bool) {
	e := float64(ext)
	outer := polyclip.Polygon{{X: 0, Y: 0}, {X: e, Y: 0}, {X: e, Y: e}, {X: 0, Y: e}}
	hole := innerQuad(rng, ext)
	if hole == nil {
		return nil, nil, false
	}
	a := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: outer, Holes: []polyclip.Polygon{hole}}}

	var rb polyclip.Polygon
	if rng.Intn(2) == 0 {
		var ok bool
		if rb, ok = forceDegenerate(rng, ext, hole, rng.Intn(3)); !ok {
			return nil, nil, false
		}
	} else {
		rb = randQuad(rng, ext)
	}
	b := polyclip.MultiPolygon{polyclip.ExPolygon{Outer: rb}}

	if len(a.Validate()) != 0 || len(b.Validate()) != 0 {
		return nil, nil, false
	}
	return a, b, true
}

// innerQuad returns a simple CW quad of 4 distinct lattice points strictly
// inside the [0,ext] square (coords in [3,ext-3]), suitable as a hole. Returns
// nil when ext is too small or no valid quad was found in a bounded number of
// attempts.
func innerQuad(rng *rand.Rand, ext int) polyclip.Polygon {
	lo, hi := 3, ext-3
	if hi-lo < 2 {
		return nil
	}
	for range 60 {
		pts := make([]polyclip.Point, 4)
		seen := map[[2]int]struct{}{}
		ok := true
		for i := range 4 {
			x, y := lo+rng.Intn(hi-lo+1), lo+rng.Intn(hi-lo+1)
			if _, dup := seen[[2]int{x, y}]; dup {
				ok = false
				break
			}
			seen[[2]int{x, y}] = struct{}{}
			pts[i] = polyclip.Point{X: float64(x), Y: float64(y)}
		}
		if !ok {
			continue
		}
		ring := polyclip.Polygon{pts[0], pts[1], pts[2], pts[3]}
		if !validQuad(ring) {
			continue
		}
		if ring.IsCCW() { // a hole must be CW
			ring.Reverse()
		}
		return ring
	}
	return nil
}

func run(sc scenario) {
	// Phase 1: generate every interacting pair SEQUENTIALLY, so the rng stream —
	// and therefore the exact set of pairs — is identical to the serial version
	// and stable across runs (toggling a library fix never shifts the pair set).
	type pair struct{ a, b polyclip.MultiPolygon }
	var pairs []pair
	for seed := range sc.seeds {
		rng := rand.New(rand.NewSource(int64(seed)*7919 + 1))
		for range sc.pairs {
			a, b, ok := genPair(rng, sc)
			if !ok || !a.BoundingBox().Intersects(b.BoundingBox()) {
				continue
			}
			pairs = append(pairs, pair{a, b})
		}
	}
	interacting := len(pairs)

	// Phase 2: run the four ops + MC oracle + checks in PARALLEL across all cores
	// (this is the slow part). Worker w handles pairs w, w+workers, w+2*workers…
	// Each pair's MC oracle uses an rng seeded deterministically by the pair index,
	// so results are reproducible and independent of worker scheduling; the
	// noise-free identity checks don't use it. Fails are collected per-worker and
	// merged — order is irrelevant, they are sorted by magnitude before display.
	workers := runtime.NumCPU()
	if workers > len(pairs) {
		workers = len(pairs)
	}
	partial := make([][]fail, max(workers, 1))
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			var fails []fail
			mcRng := rand.New(rand.NewSource(0))
			for idx := w; idx < len(pairs); idx += workers {
				a, b := pairs[idx].a, pairs[idx].b
				check := func(op string, got, want float64) {
					if math.Abs(got-want) > sc.tol {
						fails = append(fails, fail{op, a, b, got, want})
					}
				}
				gu, eu := polyclip.Union(a, b)
				gi, ei := polyclip.Intersect(a, b)
				gd, ed := polyclip.Difference(a, b)
				gx, ex := polyclip.Xor(a, b)
				if eu != nil || ei != nil || ed != nil || ex != nil {
					continue
				}
				aA, bA := a.Area(), b.Area()
				uA, iA, dA, xA := gu.Area(), gi.Area(), gd.Area(), gx.Area()
				if sc.checkMC {
					mcRng.Seed(int64(idx)*2654435761 + 12345)
					mu, mi, md, mx := mcOracle(a, b, mcRng, sc.samples)
					check("U", uA, mu)
					check("I", iA, mi)
					check("D", dA, md)
					check("X", xA, mx)
				}
				if sc.checkIdentity {
					check("idU", uA, aA+bA-iA)
					check("idD", dA, aA-iA)
					check("idX", xA, uA-iA)
				}
			}
			partial[w] = fails
		}(w)
	}
	wg.Wait()
	var fails []fail
	for _, p := range partial {
		fails = append(fails, p...)
	}

	byOp := map[string]int{}
	for _, f := range fails {
		byOp[f.op]++
	}
	fmt.Fprintf(os.Stdout, "[%s] interacting=%d gross fails=%d (U=%d I=%d D=%d X=%d idU=%d idD=%d idX=%d)\n",
		sc.name, interacting, len(fails), byOp["U"], byOp["I"], byOp["D"], byOp["X"],
		byOp["idU"], byOp["idD"], byOp["idX"])

	sort.Slice(fails, func(i, j int) bool {
		return math.Abs(fails[i].got-fails[i].want) > math.Abs(fails[j].got-fails[j].want)
	})
	n := min(len(fails), 30)
	for _, f := range fails[:n] {
		fmt.Fprintf(os.Stdout, "  %s d=%.2f got=%.3f want=%.3f A=%v B=%v\n",
			f.op, math.Abs(f.got-f.want), f.got, f.want, f.a, f.b)
	}
}

// randQuad returns a simple CCW quad of 4 distinct lattice points in [0,ext].
func randQuad(rng *rand.Rand, ext int) polyclip.Polygon {
	for {
		pts := make([]polyclip.Point, 4)
		seen := map[[2]int]struct{}{}
		ok := true
		for i := range 4 {
			x, y := rng.Intn(ext+1), rng.Intn(ext+1)
			if _, dup := seen[[2]int{x, y}]; dup {
				ok = false
				break
			}
			seen[[2]int{x, y}] = struct{}{}
			pts[i] = polyclip.Point{X: float64(x), Y: float64(y)}
		}
		if !ok {
			continue
		}
		ring := polyclip.Polygon{pts[0], pts[1], pts[2], pts[3]}
		if !validQuad(ring) {
			continue
		}
		if !ring.IsCCW() {
			ring.Reverse()
		}
		return ring
	}
}

// validQuad rejects zero-area and self-intersecting (bowtie) quads, so the
// even-odd MC containment matches the engine's nonzero semantics. A quad is
// simple iff neither pair of non-adjacent edges properly crosses.
func validQuad(q polyclip.Polygon) bool {
	if math.Abs(q.SignedArea()) < 1e-3 {
		return false
	}
	return !segCross(q[0], q[1], q[2], q[3]) && !segCross(q[1], q[2], q[3], q[0])
}

// forceDegenerate returns a simple CCW quad sharing a degeneracy with ref:
//
//	kind 0: a vertex coincident with one of ref's vertices
//	kind 1: a vertex lying ON one of ref's edges (vertex-on-edge)
//	kind 2: an edge collinear+overlapping one of ref's edges
//
// Returns ok=false when it couldn't build a valid simple quad (caller skips).
func forceDegenerate(rng *rand.Rand, ext int, ref polyclip.Polygon, kind int) (polyclip.Polygon, bool) {
	base := randQuad(rng, ext)
	idx := rng.Intn(4)
	switch kind {
	case 0:
		base[idx] = ref[rng.Intn(len(ref))]
	case 1:
		e := rng.Intn(len(ref))
		p0, p1 := ref[e], ref[(e+1)%len(ref)]
		t := []float64{0.25, 0.5, 0.75}[rng.Intn(3)]
		base[idx] = polyclip.Point{X: p0.X + t*(p1.X-p0.X), Y: p0.Y + t*(p1.Y-p0.Y)}
	case 2:
		e := rng.Intn(len(ref))
		base[idx] = ref[e]
		base[(idx+1)%4] = ref[(e+1)%len(ref)]
	}
	for i := range 4 {
		for j := i + 1; j < 4; j++ {
			if base[i] == base[j] {
				return nil, false
			}
		}
	}
	if !validQuad(base) {
		return nil, false
	}
	if !base.IsCCW() {
		base.Reverse()
	}
	return base, true
}

// segCross reports whether segments p1p2 and p3p4 properly cross (strict, no
// shared-endpoint or collinear contact).
func segCross(p1, p2, p3, p4 polyclip.Point) bool {
	d1 := orient(p3, p4, p1)
	d2 := orient(p3, p4, p2)
	d3 := orient(p1, p2, p3)
	d4 := orient(p1, p2, p4)
	return d1*d2 < 0 && d3*d4 < 0
}

func orient(a, b, c polyclip.Point) float64 {
	return (b.X-a.X)*(c.Y-a.Y) - (b.Y-a.Y)*(c.X-a.X)
}

// mcOracle returns Monte-Carlo area estimates of all four ops from a single
// sample loop over the combined bounding box.
func mcOracle(a, b polyclip.MultiPolygon, rng *rand.Rand, samples int) (u, i, d, x float64) {
	bbox := a.BoundingBox().Union(b.BoundingBox())
	w, h := bbox.Max.X-bbox.Min.X, bbox.Max.Y-bbox.Min.Y
	var cu, ci, cd, cx int
	for range samples {
		p := polyclip.Point{X: bbox.Min.X + rng.Float64()*w, Y: bbox.Min.Y + rng.Float64()*h}
		inA, inB := a.Contains(p), b.Contains(p)
		if inA || inB {
			cu++
		}
		if inA && inB {
			ci++
		}
		if inA && !inB {
			cd++
		}
		if inA != inB {
			cx++
		}
	}
	f := (w * h) / float64(samples)
	return float64(cu) * f, float64(ci) * f, float64(cd) * f, float64(cx) * f
}
