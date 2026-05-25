# polyclip — Polygon Boolean and Offset Library for Go

**Module path:** `github.com/lestrrat-go/polyclip`

A pure-Go library for 2D polygon boolean operations and offsetting. This document describes the design as built: the public surface, the algorithm, and the internals of the scanline engine (§11–§12), including the degenerate-case handling that is the bulk of the engine's complexity (§12.11).

Section numbers in §4–§12 are stable: source comments reference them (e.g. `DESIGN.md §12.11`).

---

## 1. Overview

A pure-Go library for 2D polygon operations:

- **Boolean ops** on filled polygonal regions: union, intersection, difference, symmetric difference (XOR).
- **Polygon offset** ("inflate" / "shrink"): inward and outward, with miter / round / square joins.
- Robust handling of polygons with holes, self-intersections, coincident edges, and overlapping boundaries.

The shape primitive is a simple-polygon-with-holes (`ExPolygon`) and collections of them (`MultiPolygon`); every operation is closed over `MultiPolygon`.

The downstream consumer is [`lestrrat-go/makislicer`](../makislicer), a 3D-printer slicer, where nearly every quality feature needs reliable polygon arithmetic. The reference-quality C++ library for this is **Clipper2** (Angus Johnson); the Go ecosystem lacks an equivalent. This library fills that gap.

**Goals:** correctness on adversarial input (concentric circles, self-touching polygons, collinear/coincident edges, near-degenerate slivers); pure Go (no cgo); closed (`MultiPolygon` in, `MultiPolygon` out); idiomatic small API; acceptable performance (within 5–10× of Clipper2 on slicer workloads).

**Non-goals:** 3D, general CSG/NURBS/arcs, triangulation, open-polyline offset, geometric predicates as a public API, cgo bindings to Clipper2.

---

## 2. Module layout

```
github.com/lestrrat-go/polyclip
├── polyclip.go            package doc, top-level conveniences
├── point.go               Point, BBox
├── polygon.go             Polygon, ExPolygon, MultiPolygon; winding, area, contains
├── boolean.go             Union, Intersect, Difference, Xor, UnionAll (public API)
├── offset.go              Offset, JoinType, EndType, OffsetOptions (public API)
├── validate.go            Validate, Clean
├── clip/                  scanline boolean engine (subpackage)
│   ├── segment.go         fixed-point directed-edge type, source tag
│   ├── preprocess.go      snap, dedup, overlap/T-junction splitting
│   ├── bounds.go          local-minima / bound construction, ring tracing
│   ├── event.go           event queue
│   ├── ael.go             active edge list
│   ├── sweep.go           scanline loop, DoIntersections, closeBound, lifecycle
│   ├── classify.go        winding-count classification
│   ├── poly_ops.go        IntersectEdges dispatch
│   ├── output.go          OutPt / OutRec ring construction
│   ├── horizontal.go      horizontal classification
│   ├── horzjoin.go        deferred horizontal joins
│   └── invariants.go      post-condition checks
├── fixed/                 fixed-point arithmetic (coord.go, mul.go)
├── tools/differential/    Monte-Carlo differential harness (correctness oracle)
└── examples/{union,offset}/
```

The `clip/` and `fixed/` subpackages are internal in spirit but exported within the module so tests can address them. Only the top-level `polyclip` package is stable public API.

---

## 3. Public API

The public surface is small; see the Go doc comments for full signatures.

- **Core types** (`polygon.go`, `point.go`): `Point{X,Y float64}`, `BBox`, `Polygon []Point` (implicit closing edge; outer rings CCW, holes CW by convention but either is accepted and normalized), `ExPolygon{Outer, Holes}`, `MultiPolygon []ExPolygon`.
- **Boolean ops** (`boolean.go`): `Union`, `Intersect`, `Difference`, `Xor` — each `(a, b MultiPolygon) (MultiPolygon, error)`; `UnionAll(...MultiPolygon)` for tournament-reduced multi-union.
- **Offset** (`offset.go`): `Offset(m, d, opts)` with `OffsetOptions{Join, MiterLimit, ArcTol}` and `JoinType` ∈ {miter, round, square}.
- **Utilities** (`polygon.go`, `validate.go`): `SignedArea`, `Area`, `IsCCW`, `Reverse`, `BoundingBox`, `Contains` (even-odd, boundary inside); `Clean(vertexTol, minArea)`; `Validate() []ValidationIssue`.

`error` is returned only for caller-fixable problems (e.g. a bounding box too large for the fixed-point grid, §5.1, or an offset that collapses to empty). `Validate()` issues are diagnostics, not errors.

Deliberately out of scope: open polylines, path-to-polygon clipping, standalone geometric predicates, a streaming API.

---

## 4. Algorithm

### 4.1 Boolean engine: Vatti / Clipper2 model

The engine is a Vatti scanline modeled on **Clipper2** (Angus Johnson, `CPP/Clipper2Lib/src/clipper.engine.cpp`). Clipper2 is BSL-1.0 and is used as an algorithmic reference only; this library is independently implemented under MIT (no code copied).

Plain-English sketch:

1. **Input prep** (§11.2): scale float64 input to a fixed-point integer grid (§5), split each polygon into directed edges tagged subject/clip, split overlaps and T-junctions.
2. **Local minima / bounds** (§12.1): reframe each ring as alternating ascending/descending bounds meeting at local minima/maxima.
3. **Scanline sweep** (§11.5, §12.10): maintain an active edge list (AEL) of edges crossing the current scanline; spawn bounds at local minima, advance cursors, close at maxima.
4. **Crossings** (§12.11): per scanbeam, recompute all edge crossings from the settled AEL (`DoIntersections`) and dispatch each through `IntersectEdges` (§12.5).
5. **Classification** (§11.4): each edge carries winding counts; the op + winding decides whether it bounds the result.
6. **Output** (§11.6, §11.9): contributing edges build doubly-linked rings; postprocess assigns holes and normalizes winding.

### 4.2 Boolean engine: file map

- `clip/preprocess.go` — scale/snap, dedup, overlap and T-junction splitting.
- `clip/bounds.go` — `BuildLocalMinima`, bound construction, ring tracing.
- `clip/sweep.go` — the scanline loop, `DoIntersections`, lifecycle (`closeBound`, cursor advance), degenerate-confluence handling.
- `clip/poly_ops.go` — `IntersectEdges` dispatch table.
- `clip/classify.go` — winding-count classification per op.
- `clip/output.go` — `OutPt`/`OutRec`, `AddLocalMinPoly`/`AddLocalMaxPoly`/`JoinOutrecPaths`/`SwapOutrecs`.

### 4.3 Offset engine

Offset walks each input ring once and emits an offset ring directly, vertex by vertex. With `n_i` the right-hand unit normal of edge `ring[i]→ring[i+1]` and `d` the signed distance, each vertex `v` expands based on its local turn:

1. `a = v + d·prevN`, `c = v + d·nextN` — offset endpoints of the prev/next edges at `v`.
2. `cross = prevN × nextN`; the sign of `cross·d` classifies the corner:
   - **Wedge** (`cross·d > 0`): convex offset corner; emit a join (miter apex, square chamfer, or tessellated arc) per `OffsetOptions.Join`.
   - **Overlap** (`cross·d ≤ 0`): the offset edges cross; emit the miter apex (for antiparallel normals, fall back to emitting `a` and `c`).

Holes are offset by `-d`. The raw ring is emitted unconditionally — when an inward offset overshoots the inradius it self-intersects (a pinched neck, a closing notch, an inside-out collapse) rather than being rejected.

**Topology resolution (§7.1).** Per input `ExPolygon`, the raw offset rings (outer by `d`, holes by `-d`) are checked for self/mutual intersection (`ringsIntersect`). If none, topology is unchanged and the rings are returned directly (exact, no engine pass). If they intersect, the piece is re-resolved by a **positive-fill self-union**: feed the rings to the scanline engine (`clip.SweepFill` with `clip.FillPositive`), which keeps exactly the strictly-positively-wound region — the outer winds `+1` inside, CW holes `−1` — so a pinched ring splits into islands and the negatively-wound overshoot folds drop. An inward result piece is additionally validated against the erosion definition (`insetDeepEnough`: an interior point must be ≥ `|d|` from the input boundary), which rejects the convex "inside-out" collapse whose ring is simple and positively oriented yet sits where the offset is empty. If everything collapses, `Offset` returns `ErrOffsetEmpty`.

**Degeneracy robustness.** The sweep is exact on transversal self-intersections but resolves a *snapped* degenerate configuration (same-source collinear coincident edges from parallel walls a multiple of `2|d|` apart, or a near-pinch crossing) differently — sometimes wrongly — per coordinate frame. Axis-aligned and thin-neck inward offsets hit this. So the self-union is run in several rotated frames (`selfUnionResolveAngles`) and the **most-agreed-upon** result (same piece count and area, within 2%) is kept; the correct resolution recurs across frames while each degenerate misresolution is scattered. Angle 0 (no rotation, exact coordinates) is preferred within the agreeing majority, so non-degenerate offsets keep exact output. (The boolean engine's own same-source coincident-edge gap is the deeper root cause; see §7.2.)

Direct ring construction (rather than Clipper2's "fat-edge polygons → union") avoids dense diff-source coincident-edge pile-ups and is `O(n)` for the common no-topology-change case; only intersecting pieces pay for the multi-frame self-union.

Implementation in `offset.go`: `Offset` (orchestration, hole sign, inset validation), `offsetRing` (per-ring walk), `emitVertex` (wedge/overlap dispatch), `appendMiter`/`appendMiterApex`/`appendSquareJoin`/`appendRoundJoin`, `resolveOffsetPiece` (fast path vs self-union), `selfUnionPositive`/`selfUnionAt` (multi-frame positive-fill resolution), `ringsIntersect`, `insetDeepEnough`.

### 4.4 Complexity

- Boolean: `O((n + k) log n)`, `n` = edges, `k` = intersections; the per-scanbeam `DoIntersections` is `O(m²)` per beam of `m` active edges (correctness-first; a merge-sort inversion counter is the later optimisation).
- Offset: `O(n)` per ring plus `O(n·m)` for the inward-overshoot check (early-exits on the first failing vertex).

---

## 5. Numeric robustness

This separates a working library from a fragile one; treat it as load-bearing.

### 5.1 Fixed-point internal representation

User input is float64; the engine scales to `int64` on a uniform grid: `internalCoord = int64(round(userCoord * Scale))`. `Scale` is chosen per-operation from the input bounding box so all intermediate products (intersection determinants are degree-2 in coordinates) stay in range (`int128` synthesized from two `int64`s for the high-precision determinant). Output is descaled to float64.

Integer coordinates eliminate the most common Vatti failure: "the same point computed two ways gives slightly different float64s and breaks topology."

### 5.2 Robust segment intersection

Two segments cross per the signs of orientation determinants:

```
det(P, Q, R) = (Q.X - P.X)*(R.Y - P.Y) - (Q.Y - P.Y)*(R.X - P.X)
```

Computed exactly via the `int128` helper in `fixed/mul.go`; **never** in float64. The intersection *point* needs division and may be computed in float64 / rounded to the grid — only the orientation predicates need full precision. (Reference: Shewchuk 1997; integer coordinates give exact orientation, so the simpler "exact integer predicates" approach suffices.)

### 5.3 Coincident edges

Collinear overlapping edges are split in preprocess so the sweep only ever sees *fully* coincident pairs (§11.7), then resolved by the winding rule and the coincident-horizontal handling in §12.11.

### 5.4 Vertices on edges (T-junctions)

A vertex of one polygon lying on an edge of another is exactly representable on the integer grid. `SplitTJunctions` (preprocess) splits the edge at that vertex up front, so the sweep never faces an ambiguous mid-edge vertex. This is the single most common Vatti bug source in implementations that skimp on §5.1.

### 5.5 Self-intersecting input

Self-intersecting input rings are accepted; Vatti's intersection step processes every crossing, including self-crossings. Output is always simple.

---

## 6. Testing

- **Unit tests** alongside each source file: predicates, segment intersection, AEL/event-queue operations, ring assembly, per-join offset geometry.
- **Adversarial cases** in `boolean_adversarial_test.go` / `boolean_test.go`: overlapping squares, square-minus-square annulus, edge- and vertex-touching unions, self-touching "8", concentric rings, T-junctions, hole–clip confluences (`TestBooleanHoledInput…`). Each asserts the set identities within tolerance.
- **Fuzzing** (`fuzz_test.go`, `testing.F`): random polygons checked against invariants — `Union(A,A)=A`, `Difference(A,A)=∅`, `Xor(A,B)=Union(Diff(A,B),Diff(B,A))`, and `Area(A∪B)=Area(A)+Area(B)−Area(A∩B)`. (The fuzz corpus under `testdata/fuzz/` is gitignored.)
- **Differential harness** (`tools/differential`, run `go run ./tools/differential`): the standing correctness oracle. It generates random and forced-degenerate input pairs and checks the **noise-free set identities** `idU = U−(A+B−I)`, `idD = D−(A−I)`, `idX = X−(U−I)` (which must be zero) plus absolute areas against a Monte-Carlo oracle. See §12.11 for why Clipper2 is *not* used as the reference. The four buckets (`random-small`, `random-large`, `degenerate`, `holes`) currently report `idU=idD=idX=0`.

---

## 7. Roadmap / TODO

The boolean engine (§11–§12) is correct on the noise-free set identities
across the random, large, degenerate, and holed differential buckets
(`idU=idD=idX=0`, §6) and is considered slicer-grade. The items below are the
known gaps between current state and a complete drop-in for `makislicer`,
roughly in priority order.

### 7.1 Offset: inward-offset topology changes — DONE

`Offset` now re-resolves topology changes via a per-piece positive-fill
self-union (§4.3). A dumbbell offset inward past its neck yields the two
island pads; a U-shape / notch that closes resolves to the correct connected
shape; an over-shrunk convex ring collapses to empty. Validated by
`TestOffsetDumbbellSplits` (eight orientations), `TestOffsetUNotchCloses`, and
`TestOffsetInwardErosionOracle` (a Monte-Carlo erosion oracle over random
concave polygons), with the existing offset suite unchanged and the boolean
differential still `idU=idD=idX=0`.

Residual: robustness leans on a multi-frame (rotated) majority vote because the
boolean sweep mis-resolves *snapped* same-source collinear coincident edges
(common in axis-aligned and thin-neck inward offsets). The clean fix is to make
the sweep handle same-source coincident edges directly under `FillPositive` —
the in-sweep gap detailed in §7.2. Until then the multi-frame vote (≈8 sweeps
per topology-changing piece) is the cost; non-topology-change offsets keep the
exact `O(n)` fast path.

### 7.2 Public `Simplify` — DONE; in-sweep coincident fix remains

**Public entry point (DONE).** `Simplify(m MultiPolygon)` (`boolean.go`) runs
the Vatti engine over `m` as a single source (`clip.Sweep`, non-zero fill),
bypassing the `mpolyEqual` idempotency short-circuit that made the old "run
`Union` of `m` with itself" advice a no-op. A figure-eight splits into its two
oppositely-wound loops, a doubly-traced ring collapses to one, a doubled-back
spur cancels. Transversal (general-position) self-intersections resolve
exactly; the snapped collinear degeneracy below is the residual limit. The
misleading doc comments in `polygon.go` (`ExPolygon`, `Clean`) and `validate.go`
(`IssueSelfIntersecting`) now point at `Simplify`. Tests: `simplify_test.go`.

**In-sweep same-source coincident edges (REMAINS — the deeper root cause).**
The boolean sweep still mis-resolves a *single* self-overlapping ring with
same-source collinear coincident edges (axis-aligned and thin-neck inward
offsets produce them; it is why §7.1's offset self-union needs the multi-frame
rotated majority vote). The minimal repro is the dumbbell offset by `-2`: its
raw offset ring traces the left-pad right wall twice over `y∈[4,6]`, so
`SplitOverlaps` makes a doubled coincident edge there. Investigation
(2026-05-24) pinned the mechanism precisely:

- A self-overlapping ring's winding genuinely reaches 2 across a doubled wall;
  the pad interior is `+1`, the dropped neck fold `−1`, and the doubled wall is
  a `+1 → 0 → −1` step represented by two coincident unit edges. So the wall's
  true boundary edge has `WindSelf == 0` on its right (the infinitesimal sliver),
  not `1`.
- `DedupCoincidentEdges` collapses the doubled edge to one, destroying the `+1`
  count the second pass carries — making winding locally inconsistent (the
  sweep then emits 3 spurious pieces / area 40 instead of 2 pads / area 72).
  Keeping both edges instead is necessary but not sufficient.
- `FillPositive`'s contributing test is `WindSelf == 1` (exact), which wrongly
  drops the `+1/0` boundary edge; the correct rule is a winding-`>0` *boundary*
  test, `(WindSelf > 0) != (WindSelf - WindDx > 0)`.
A second investigation (2026-05-24, WIP branch `fix-positive-fill-coincident`)
disproved the "keep both edges + boundary test + pure incremental" theory as
*sufficient* and resolved the problem into three coupled layers:

- **L1 — winding core.** Under `FillPositive`/`FillNegative` `Classify` must
  compute `WindSelf` as a pure signed prefix sum of same-source `WindDx` (the
  NonZero "reversing direction" heuristic wrongly keeps `WindSelf` at magnitude
  1 across a doubled wall instead of stepping `+1 → 0 → −1`); the contributing
  test must be the boundary form `(WindSelf>0) != (WindSelf−WindDx>0)`; and the
  `poly_ops.go` incremental update must be a pure `+=`/`−=` (no NonZero
  reflection). All gated on the fill rule so the boolean differential is
  untouched. *Implemented and verified correct on the vertical walls.*
- **L2 — bound reconstruction.** `BuildLocalMinima`'s segment-soup walker
  (`traceRing` via input-direction adjacency) cannot disambiguate the
  *collinear degree-4 vertices* the doubled wall creates after `SplitOverlaps`
  (two identical out-edges at one vertex, indistinguishable by angle), so it
  traces spurious sub-cycles. The fix is to build local minima from the ring in
  *traversal order* (Clipper2-style), splitting each ordered segment only at
  the endpoints the soup passes introduce — the two wall passes then occupy
  distinct sequence positions. *Implemented as `SweepRingsFill` /
  `splitOrderedRings` (`clip/sweep_ordered.go`); with L1+L2 the canonical
  dumbbell's left island resolves exactly (area 36).*
- **L3 — exact-coincidence ambiguity (FUNDAMENTAL — the vote is the answer).**
  Two parts. (a) A local-min bound that *leads with a horizontal* is ordered in
  the AEL at the horizontal's near X, so the prefix sum runs before the bound's
  real wall is placed. *Fixed* by ordering the prefix sum by each bound's
  position just **above** the scanline (the far end of a leading horizontal);
  this resolves the dumbbell's left island and excludes the neck from it.
  (b) The residual is fundamental: two **exactly coincident** ascending walls
  over a Y-range (the dumbbell's `x=22` over `y∈[4,6]` — the right square's
  left wall and the neck's right wall) are geometrically indistinguishable at
  the local-min scanline. Being parallel they *never cross*, so no event ever
  orders them, and the winding prefix cannot tell which carries the `0` sliver
  vs the `+1` boundary. Deferring the ring-start decision does not help (no
  ordering event ever arrives); a topology look-ahead to where the walls
  diverge is itself degenerate because they diverge through horizontals at the
  same `Y`.

The standard computational-geometry resolution for such exact degeneracies is
**perturbation** — and the multi-frame rotation vote (§7.1) *is* perturbation
that breaks the coincidence so a clean transversal sweep resolves it. So
"retire the vote" is reframed: the vote is the **correct design** for the
exact-coincidence residual, not a stopgap, until/unless the engine gains
principled symbolic perturbation (Simulation-of-Simplicity style) letting a
single sweep break ties deterministically.

**Crossing-dispatch restructure (DONE).** `Offset` now runs its self-union on
the ordered-minima engine (`SweepRingsFill`) plus the rotation vote, replacing
the soup path. The blocker that previously made the ordered path worse —
transversal self-crossings (rotated pinches) merging into one island — was a
NonZero assumption in `IntersectEdges`: `branchNeitherHot` and the edge
eligibility guard keyed on `absInt(WindSelf) == 1`, which drops a positive-fill
boundary whose `WindSelf` is `0` (the doubled-wall sliver). Under `AEL.Ordered`
both are now driven by the `Contributing` (winding-`>0` boundary) flag instead;
general-position self-intersections resolve at a single sweep, and the vote is
needed only for the exact-coincidence residual above. All gated on
`AEL.Ordered`, so the boolean (`FillNonZero`) path is untouched (differential
`idU=idD=idX=0`). See `docs/offset-coincidence-perturbation.md`.

### 7.3 Performance

Benchmarked on representative slicer geometry (`perfbench_test.go`: disjoint
contours, big circles, staggered brick walls, meshing gears). The bottleneck
depends on the input shape:

- **Sparse / disjoint / axis-aligned inputs** (the common slicer-layer case)
  were dominated — ~95% of CPU — by the `O(n²)` preprocessing pair scans, NOT
  by the scanbeam. `SplitOverlaps` and `SplitTJunctions` now resolve in a
  single batch pass each: `SplitOverlaps` buckets segments by their exact
  (128-bit) supporting line and splits within a line bucket; `SplitTJunctions`
  cuts each segment at the interior vertices found through an X-sorted vertex
  index. This dropped those benchmarks 24–89× (e.g. a 24×24 brick wall union
  178 ms → 2 ms) with no change to the differential oracle.
- **Dense mutually-intersecting inputs** (meshing gears) were then dominated
  (~87% of CPU) by `buildIntersectList`, the per-scanbeam crossing enumeration
  (§4.4). It is now a merge-sort inversion counter (à la Clipper2
  `BuildIntersectList`): a proper crossing in the beam swaps the two edges'
  X-order between the beam bottom and top, so the crossing pairs are the
  inversions between the bottom and top orderings, enumerated in
  `O(n log n + k)` instead of testing all `O(n²)` pairs. This cut the gears
  benchmarks ~3.8× (union 75 ms → 20 ms) with no change to the differential.

  Edge ordering uses exact 128-bit rational X-intercept comparison
  (`fixed.CmpRationals`, via `clip.cmpXAtY`), **not** the float `XAtY`: at the
  ±`MaxCoordMagnitude` grid a float intercept carries hundreds of units of
  rounding error, enough to mis-order a crossing on a scanline and drop it.
  Two boundary cases are added back as candidates so the node set matches the
  exact full scan: edges concurrent at the beam bottom (no defined order there)
  and AEL-adjacent pairs (nearly-parallel edges whose true crossing lies just
  outside the beam but whose float crossing point — still used for the beam
  test, matching the old behaviour — rounds inside). Validated by a full-scan
  cross-check assertion run over the entire differential corpus.

The "within 5–10× of Clipper2" goal (§1) is still not measured against
Clipper2 directly (the differential oracle is Monte-Carlo, not Clipper2).

### 7.4 Open-path offset (`EndType`)

`EndType` is a reserved stub; only `EndPolygon` is implemented. Slicers want
open-polyline offset for thin-wall / gap-fill / single-extrusion features.
Currently a §1 non-goal — listed here so the scope decision is explicit and
revisitable.

### 7.5 Reachable `ErrHorizontalNotSupported`

The legacy per-edge fallback can still return `ErrHorizontalNotSupported` on
shared-vertex inputs where `BuildLocalMinima` fails (§12.10.7, `boolean.go`).
Axis-aligned features are common in printed parts, so callers must handle the
error path.

**Reachability — MEASURED, the fallback IS heavily reachable (do NOT retire it
as-is).** `TestHorizontalFallbackReachability` runs ~78k boolean ops over
random axis-aligned *skyline* polygons (dense in mid-bound horizontals) whose
overlap creates shared vertices: ~9.1k fall back (`BuildLocalMinima` returns
`ErrOpenRing` — "chain revisits … before closing the ring") and ~8.9k of those
then surface `ErrHorizontalNotSupported`. So the bound model does **not** yet
cover shared-vertex axis-aligned inputs: after preprocessing creates a degree-4
collinear vertex, `BuildLocalMinima`'s segment-soup `traceRing` can't
disambiguate the ring, and the legacy `ClassifyHorizontals` then rejects the
staircase (mid-bound) horizontals those polygons contain. Retiring the fallback
requires making minima reconstruction robust to shared vertices — the same L2
ordered-ring reconstruction (`clip/sweep_ordered.go SweepRingsFill`,
`splitOrderedRings`) built for §7.2, wired into the boolean path. That is the
real §7.5 fix and is left for a future increment; until then the error path
stands and is documented.

**`processHorzJoins` infinite-loop hang — FIXED.** The reachability harness
first surfaced a *hang* (not an error) on `Difference` of two skylines: the
merge branch of `processHorzJoins` re-threaded only `or1`'s original arc
(`op1b → j.op1`) of the unified cycle, leaving `or2`'s arc still pointing at the
released (dead, `Pts==nil`) `or2`. A later join then read that stale OutRec,
mis-detected a same-ring split as a cross-ring merge, and spliced one cycle into
a broken one — an unterminating re-thread walk. Fix: re-thread the **entire**
unified cycle (matching `JoinOutrecPaths`), so no OutPt is left on the dead
ring and the `or1==or2` split/merge test always reads live pointers. Differential
byte-identical (idU=idD=idX=0, gross 93/236). Regression: `TestHorizJoinHangRepro`.

### 7.6 Axis-aligned identity violations (collinear shared edge) — PARTIAL

Pre-existing, distinct from §7.5 (surfaces *after* the §7.5 fallback succeeds,
on inputs the bound model DOES handle). A class of algebraic-identity violations
on axis-aligned pairs sharing collinear boundary segments, dominated (67 of the
original 73) by a **coincident cross-source vertical wall** (one polygon's wall
lies exactly on the other's, overlapping in Y). Two fixes have landed, cutting
`TestHorizontalFallbackReachability`'s logged violations from ~106 → 73 → **44**:

1. **Intersect spurious-lobe** (below) — the outer-max coincident-horizontal apex.
2. **Coincident AEL edge ordering by divergence** (`aelLess` / `coincidentDivergeLess`,
   `clip/ael.go`). Two exactly-coincident edges tie on both CurrX and slope, so a
   static order is arbitrary and decides each wall's `WindOther` (hence which
   contributes) — wrongly, because the correct order is context-dependent. The
   geometrically correct order is where the two bounds first *diverge* just above
   the scanline (Clipper2's collinear-edge handling): look ahead along both
   bounds' upward vertex paths to the first differing vertex and order by which
   ray runs left (128-bit cross product — the 2^60 grid overflows int64). A bound
   that tops out at the divergence contributes a synthetic turn vertex from its
   `WindDx` (interior side). Differential byte-identical (idU=idD=idX=0).

**Residual (44):** the spurious lobes are half-integer (diagonal) artifacts where
a Difference/Union ring mis-traces *up* a coincident wall reached via through-vertex
advancement (which does not reclassify), then pinches into a diagonal spur. The
remaining fix is a coincident-wall **ring redirect** at the confluence (turn the
boundary onto the horizontal seam instead of climbing the doubled wall). NOTE: a
blunt reconcile that dispatches the coincident pair through `IntersectEdges`
CRASHES (nil deref — the crossing dispatch assumes a transversal cross, not a
doubled non-crossing boundary); the redirect needs custom topology handling.

**Intersect spurious lobe — FIXED.** The symptom was an algebraic-identity
violation on axis-aligned pairs sharing a collinear boundary segment — e.g.
`A=[(0,0)(2,0)(2,1)(1,1)(0,1)]`, `B=[(1,-1)(3,-1)(3,3)(2,3)(2,1)(1,1)]` (sharing
(1,1)-(2,1)): `Union` returns 7 and `Intersect` returns 2, so `U = A+B−I`
breaks. The root cause was in **`Intersect`, not `Union`**: the true union *is*
7 (A=2, B=6, true I=1), and the bug was that Intersect emitted a **spurious
second lobe** — a triangle `(2,1)-(3,3)-(2,3)` lying inside B's upper-right
region but entirely *outside* A — making I=2 instead of 1.

The shared segment (1,1)-(2,1) is **A's outer local maximum**: A's two bounds
meet there and A is absent above it. The intersection ring (the unit square
[1,2]x[0,1]) should close along that edge. Instead, when A's right wall reached
the apex, `closeBound`'s cross-source self-closure check (which removes the
maxing edge and reclassifies the coupled hot edge to test whether the region
continues above) was fooled: the coupled edge reaches the apex along a
*coincident horizontal*, and A's *other* bound (the left wall) is still in the
AEL at that scanline — it tops out at the same Y but at a different X/event — so
the scanline reclassify still counts A as present and kept the ring open. B's
hot bound was then dragged up out of A, threading the spurious lobe (a figure-8
pinched at (2,1) that `splitSelfTouchingRings` split into the square + the stray
triangle).

Fix (`clip/sweep.go`): when the coupled edge reaches the apex along a coincident
horizontal, decide "continues above" with `otherSourceWindingAbove` — a winding
probe at the apex column counting only edges that span **strictly above** the
scanline (so a same-Y local maximum of the other source does not register as
present). When the other source is absent above, the coincident edge is the top
of the intersection region and the ring closes. Scoped to `OpIntersect`;
differential byte-identical (idU=idD=idX=0, gross 93/236). Regression:
`TestHorizIdentityRepro`. This cut the reachability harness's logged identity
violations from ~106 to 73 (the remainder are the residual staircase cases).

---

## 8. Conventions and constraints

- **Go version:** 1.22+. Concrete types in the engine; generics only where they pay.
- **Dependencies:** zero external modules — standard library only. Non-negotiable; the point is to be a clean leaf dependency.
- **Concurrency:** the public API is safe for concurrent use on different inputs. A `MultiPolygon` value is not synchronized (same rule as `[]int`). No internal parallelism in the library itself.
- **Style:** `gofmt`/`go vet`/`staticcheck` clean; errors wrap `fmt.Errorf("polyclip: …: %w", …)`; public symbols have doc comments; no package-global mutable state; no working `init()`.
- **Pitfalls that look tempting but are wrong:** float64 coordinates in the sweep (breaks topology, §5.1); copying Clipper2 source (license); Greiner-Hormann (can't handle coincident edges); offset via miter math without an engine (the naive approach this library replaces).

## 9. References

1. Vatti, B. R. (1992). *A Generic Solution to Polygon Clipping.* CACM 35(7), 56–63.
2. Johnson, A. *Clipper2.* https://github.com/AngusJohnson/Clipper2 — algorithmic reference, BSL-1.0; not copied.
3. Shewchuk, J. R. (1997). *Adaptive Precision Floating-Point Arithmetic and Fast Robust Geometric Predicates.* DCG 18(3), 305–363.
4. Martínez, F., Rueda, A. J., Feito, F. R. (2009). *A new algorithm for computing Boolean operations on polygons.* (Rejected alternative.)

---

## 11. Sweep engine: pipeline, data model, output

### 11.1 Pipeline

```
MultiPolygon (subject, clip; float64)
        │  preprocess (§11.2)
[]Segment  (fixed-point, source-tagged; overlaps & T-junctions split)
        │  sweep (§11.5, §12)
OutRec rings (doubly-linked, closed)
        │  postprocess (§11.9)
MultiPolygon (fixed-point → float64)
```

Each stage lives in its own file and does not know the next stage's internals.

### 11.2 Preprocess (`clip/preprocess.go`, `boolean.go appendRing`)

1. Compute the union bbox of subject+clip; build one `fixed.Scale` (§5.1).
2. Per ring, snap each vertex and emit a `Segment` per edge, tagged subject/clip. Normalize orientation (CCW outer / CW hole) by reversing a ring whose signed area disagrees, so the engine's "WindDx from traversal direction" assumption holds for any input winding.
3. Drop degenerate segments; `simplifyCollinearRing` removes collinear-through input vertices (an exact no-op that removes spurious bound turns).
4. `SplitOverlaps` — establishes "no partial collinear overlaps" (only fully coincident pairs remain).
5. `SplitTJunctions` — establishes "no vertex lies in the open interior of any edge" (§5.4). Split points are existing grid vertices, so area-preserving.
6. `DedupCoincidentEdges` — resolves same-source coincident pairs (§11.7).

### 11.3 Sweep state: the ActiveEdge

An `ActiveEdge` carries (see §12.2 for the full struct):

- `WindSelf` — signed winding count of this edge's source up to and including this edge.
- `WindOther` — signed winding count of the *other* source to the left (exclusive).
- `WindDx` — signed input-traversal direction, ±1, set once at spawn and shared by every segment of the bound (including horizontals). `Classify` uses `WindDx` as the contribution; this is what lets a horizontal carry its bound's contribution while in the AEL.
- `Contributing` — whether this edge bounds the result (set by `Classify`).
- `Outrec` — non-nil iff the edge is "hot" (building a ring).

Winding is computed at spawn from the left neighbour and updated *in place* at each crossing by `IntersectEdges` (§12.5) — no left-walk recompute.

### 11.4 Classification table

An edge contributes iff `inside(left) ≠ inside(right)`, where `inside(side) = inside_subject(side) OP inside_clip(side)`. The other source's count is identical on both sides (only this edge's source flips across it), so:

| Op         | Contributes iff                                                        |
|------------|------------------------------------------------------------------------|
| Union      | `WindOther == 0` AND self-count flips across the edge                  |
| Intersect  | `WindOther != 0` AND self-count flips                                  |
| Difference | subject edge: `WindOther == 0` AND flip; clip edge: `WindOther != 0` AND flip |
| Xor        | self-count flips (every boundary flip contributes)                     |

`Classify` (`clip/classify.go`) transcribes Clipper2's `SetWindCountForClosedPathEdge` (NonZero rule); `isContributing` is `IsContributingClosed`.

### 11.5 The scanline loop

`sweep.run()` processes events in `(Y, X, Kind)` order. Per scanline it: resolves all crossings strictly inside the scanbeam from the settled AEL (`DoIntersections`, §12.11), spawns bounds at local minima, advances/closes bounds at Tops (§12.10), flushes horizontals (§12.6), and reconciles at-vertex crossings. The crossing model is per-scanbeam recompute, **not** incremental scheduling (§12.11). Ring construction (`AddLocalMinPoly`/`AddLocalMaxPoly`/`AddOutPt`/`SwapOutrecs`, §12.3–§12.5) happens inside the crossing and close handlers.

### 11.6 Output ring data structure

```go
type OutPt struct { P fixed.Point; Next, Prev *OutPt; Outrec *OutRec }
type OutRec struct {
    FrontEdge, BackEdge *ActiveEdge // the two edges currently building the ring
    Pts                 *OutPt      // one vertex of the cycle; nil when merged away
    fromInputMin        bool        // spawned at an input local min vs a crossing (§12.11)
    // … Owner/IsHole set by postprocess
}
```

`FrontEdge` contributes to the head of the chain (prepend), `BackEdge` to the tail (append). Both are nil once the ring closes. Two contributing edges of the same ring just emit points on their respective ends; two edges of different rings meeting are spliced by `JoinOutrecPaths`.

### 11.7 Coincident edges

After §11.2, the only collinear pairs the sweep sees share both endpoints:

- Same source, same direction (duplicate) — dropped in preprocess.
- Same source, opposite direction — cancel; dropped.
- Different source — handled by the coincident-horizontal rules in §12.11 (a doubled/cancelling boundary skips the transversal-crossing dispatch and is reconnected by the horizontal-join pass), and by the winding rule for sloped coincident pairs.

The bound model places each source's leading horizontal in a separate bound; the at-vertex and coincident-horizontal handling in §12.11 (`reconcileSharedVertexCrossings`, `dispatchIntersect`'s opposite-interior skip, `processHorzJoins`) recovers the correct topology. `Union(A, A)` and analogues — where every edge becomes a same-vertex diff-source coincident pair — are short-circuited at the API level (`boolean.go mpolyEqual`).

### 11.8 Horizontal segments

A horizontal has `Bot.Y == Top.Y`. In the bound model a horizontal is a first-class AEL member carrying its bound's `WindDx` (§12.6.1), processed by the `DoHorizontal` pass (§12.6) after the Top events at its scanline. Event ordering at one `(Y, X)` is `Top < Bot/LocalMin < Horiz < Intersection`: closing edges leave first, new bounds enter, horizontals then walk the settled AEL. Coincident horizontals are pre-split (§11.2) so the pass never sees partial overlaps.

### 11.9 Postprocess (`clip/bounds.go` ring tracing, `assembleResult`)

After the sweep, each closed `OutRec` becomes a `Polygon` (walk the cycle, dedup consecutive equal points). Hole nesting is computed by a **containment forest over all output rings** (any orientation), classifying each ring by depth parity: even depth = a filled region (ExPolygon outer), odd depth = a hole of its (even-depth) parent. The sweep's own CW/CCW orientation is used only to normalize the final winding (filled→CCW, hole→CW), never to classify — an island inside a hole is CCW yet at depth 2 and must become its own top-level piece.

Containment is sampled at a **genuine interior point** (`interiorPoint`: midpoint of the widest interior span on a scanline that grazes no vertex and runs along no horizontal edge), not a vertex centroid — a centroid on a shared/collinear edge would test boundary-inclusive `Contains` as inside and falsely nest touching rings. Only a strictly *larger* ring can be a container, except that two equal-area **coincident** rings (the same boundary emitted once CCW and once CW, as Difference/Xor of identical inputs produce) break the tie by orientation and cancel to zero area. Finally, fixed-point coordinates are unsnapped to float64.

### 11.10 Invariants

Checked as post-conditions by `clip.CheckInvariants` (from `clip/invariants_test.go`):

1. **AEL ordering:** after each event the AEL is sorted left-to-right by `CurrX` (slope tie-break). (`clip.CheckAELSorted`.)
2. **WindSelf bounded** by the number of input rings of that source.
3. **Ring cycles well-formed:** every closed ring's `Next`/`Prev` links round-trip and `OutPt` back-pointers are consistent.
4. **Rings close or retire:** at sweep end every `OutRec.Pts` is either a closed cycle or nil (retired via `JoinOutrecPaths`); no partially-open rings.

---

## 12. Sweep engine: bounds, dispatch, horizontals, degeneracies

This chapter distills the parts of the algorithm translated from Clipper2 (`CPP/Clipper2Lib/src/clipper.engine.cpp`; file:line references point into that tree). Clipper2 is BSL-1.0 — algorithmic reference only, written from scratch under MIT.

### 12.1 Bounds and local minima

Each input polygon is reframed as alternating **ascending** and **descending bounds** — chains of edges monotonic in Y. A **local minimum** is where a descending bound meets an ascending one (the ring turns from down to up); a **local maximum** is the inverse. A bound is a single AEL entry that advances through its edges via in-place cursor advance (§12.10.4), rather than one event per edge.

`BuildLocalMinima` (`clip/bounds.go`) walks each ring, finds every Y-direction reversal (horizontals count toward their non-horizontal neighbours' direction), and emits a `LocalMinima` record with its two emerging bounds, sorted by Y-ascending (X for ties) — the event processing order. polyclip is non-polytree: hole nesting is recomputed in postprocess (§11.9), open paths are out of scope, and the only fill rule is NonZero.

### 12.2 ActiveEdge / OutRec fields

`ActiveEdge` (mirroring Clipper2's `Active`) carries: `Bound` + `EdgeIdx` cursor (geometric, set once at spawn, never reassigned); `CurrX`; `WindSelf`/`WindOther`/`WindDx` (§11.3); `Contributing`; `Outrec` (logical — non-nil iff hot, changes at crossings). Key invariant: **`Bound` is geometric, `Outrec` is logical** — cursor advance consults only `Bound`/`EdgeIdx` and never touches `Outrec`.

`OutRec` carries `FrontEdge`/`BackEdge` (§11.6). `IsFront(e)` ≡ `e.Outrec.FrontEdge == e`; many `IntersectEdges`/`AddLocalMaxPoly` decisions branch on it.

### 12.3 AddLocalMinPoly (`engine.cpp:1332`)

Called when two AEL edges become the two sides of a new contributing ring. Allocates an `OutRec`, assigns both edges to it, and decides which is `FrontEdge`/`BackEdge` from the nearest prior hot edge and the `isNew` flag (true for a real input minimum, false for a synthetic minimum created by a crossing). `fromInputMin` is recorded from `isNew` (used by §12.11's figure-8 discriminator).

**Convention note:** polyclip's `FrontEdge` is the RIGHT/descending side (the mirror of Clipper2's "front = leftmost"), so its `Pts` cycle reads CCW. Callers pass `(rightAE, leftAE)`; downstream code depends only on the FrontEdge/BackEdge identity, and postprocess (§11.9) determines orientation independently from signed area.

### 12.4 AddLocalMaxPoly & JoinOutrecPaths (`engine.cpp:1380`, `1435`)

Called when two edges meeting at a local maximum close (or merge) their ring(s). If both belong to the same `OutRec`, the ring closes and both edges uncouple. If they belong to different `OutRec`s, `JoinOutrecPaths` splices the two doubly-linked chains into one and discards the second. The same-side (both-front / both-back) cases are the figure-8 and reverse-join handling described in §12.11.

### 12.5 IntersectEdges dispatch (`engine.cpp:1772`)

The heart of the engine (`clip/poly_ops.go`). At a crossing of two adjacent AEL edges it updates both winding counts in place, refreshes `Contributing`, then dispatches on `(e1Hot, e2Hot)` and the post-update winding:

- **Both hot:** close/join via `AddLocalMaxPoly`, or (interleave) `AddOutPt` on each + `SwapOutrecs`.
- **Exactly one hot:** `AddOutPt` on the hot edge, then `SwapOutrecs` (the cold edge inherits the ring).
- **Neither hot:** `AddLocalMinPoly` spawns a new ring when the op + winding makes the crossing a region entry.

The AEL position swap is deferred to the *end* of `IntersectEdges` (Clipper2 dispatches in pre-crossing order then swaps); `AddLocalMinPoly` resolves left/right from current AEL positions and walks `getPrevHotEdge(left)`, so orientation is argument-order-independent. `dispatchIntersect` also handles the coincident-horizontal skip (§12.11).

### 12.6 DoHorizontal (`engine.cpp:2526`)

When a bound's cursor reaches a horizontal, `DoHorizontal` walks the AEL across the horizontal's X-span: at the bound's local-max vertex it calls `AddLocalMaxPoly`; for a contributing crossing it dispatches through `IntersectEdges`; otherwise it advances the cursor. Horizontals are queued during their scanline and processed after the Top events (§11.8 ordering), so the AEL is settled when they walk.

### 12.6.1 Horizontals as first-class AEL edges

Horizontals live in the AEL like any other edge (there is **no** separate synth-intersect path — every crossing flows through the single `IntersectEdges` model, §12.5). The crux is the winding model: a horizontal must carry the `WindDx` (±1) of the **bound it belongs to** — the sign of the bound's adjacent non-horizontal edge — not 0. A horizontal does not change the winding of a vertical ray it lies along, but it must carry its bound's contribution forward so neighbours classify correctly while it sits in the AEL.

Consequences: `spawnBoundActive` does not skip a leading horizontal (the bound's `ActiveEdge` sits on its first segment even when horizontal); `advanceBoundCursor` walks onto a horizontal in place (§12.10.4); a horizontal local minimum's two ascending bounds enter the AEL first, then the horizontal pass calls `AddLocalMinPoly` on them. The event order is `Top < Bot/LocalMin < Horiz < Intersection` (§11.8).

### 12.7 Local-minima pre-pass

Before the sweep, `BuildLocalMinima` (§12.1) walks each ring once, finds every Y-direction reversal, and emits sorted `LocalMinima` records. Horizontals count toward their non-horizontal neighbours' direction.

### 12.10 ActiveEdge lifecycle

Several state machines coexist in `handleTop`: cursor advance, intersection swaps, OutRec front/back rewiring, AEL ordering. The rules below are load-bearing.

#### 12.10.1 Scanbeam loop

```
loop:
  DoIntersections(botY, y)   // ALL crossings strictly inside the beam (§12.11)
  spawn bounds at local minima at y
  advance/close bounds at Tops at y
  flush horizontals queued at y
  reconcile at-vertex crossings
```

Crossings are processed strictly *inside* the scanbeam `(botY, y)` — even an intersection the algebra puts at the exact top is clamped into the beam (`engine.cpp:2353`); no crossing ever fires at the same Y as a Top.

#### 12.10.4 Cursor advance (`advanceBoundCursor`)

When a non-horizontal edge reaches its Top without ending its bound, advance the cursor to the next segment **in place** — emit the local-max vertex if hot, then update `Seg`/`EdgeIdx`/`CurrX` and queue the next Top (or, for a horizontal next segment, queue the horizontal pass). **Do not** remove/reinsert in the AEL: the slope may change, but AEL order is fixed by the next scanbeam's `DoIntersections`. This mirrors Clipper2's `UpdateEdgeIntoAEL` (`engine.cpp:1731`). After advancing, schedule fresh intersection checks against the new segment's neighbours (its slope differs from the old). A backward bound carries `Reversed=true`.

#### 12.10.5 Close (`closeBound`)

Called when a bound's cursor reaches its last segment (or walks through its trailing horizontals). The local-max vertex is the last segment's `Top`, or the far endpoint of the trailing horizontal. The two bounds of the maximum may reach it at the same event (close together via `AddLocalMaxPoly`) or at different events (one runs Case A — emit the apex, remove from the AEL, leave the OutRec coupling for the partner's later Case B close). `closeBound` is also where the degenerate-confluence handlers fire (§12.11): handoff-through-vertex, the maxima/between-maxima pairing, the Intersect/Difference notch-plateau joins, and the figure-8 same-side close.

#### 12.10.7 Load-bearing rules

- **Maxima gate on `IsHotEdge`, not `Contributing`.** A post-swap reclassification can leave an edge non-contributing yet still hot; its ring must still close/join, or the top half of an overlapping-shapes union is dropped.
- **Cursor advance must reschedule intersections.** The new segment may cross neighbours the old one didn't; without `maybeScheduleIntersect` against the new neighbours, a crossing silently never fires.
- **`BuildLocalMinima` is tried before `ClassifyHorizontals`.** The bound model handles mid-bound horizontals natively (a staircase); the legacy `ClassifyHorizontals` rejects them. The bound model is used whenever `BuildLocalMinima` succeeds.

### 12.11 Degenerate-confluence handling

General-position crossings are handled by the **per-scanbeam `DoIntersections` recompute**: for each scanbeam `(botY, topY]`, recompute *all* crossings from the settled AEL, sort by `(pt.Y, pt.X)`, and dispatch via `IntersectEdges`; if rounding leaves a node's edges non-adjacent, advance to the next adjacent node first (Clipper2 `ProcessIntersectList`). This replaced an earlier incremental "schedule-on-adjacency-change" scheduler that silently lost crossings whenever an adjacency formed without a fresh pairwise check.

The residual complexity is **degeneracies**: shared vertices, vertices on edges, and collinear/coincident edges — especially where a subject hole and the clip meet. Every such failure becomes correct under off-grid perturbation, confirming it is a snap/degeneracy effect, not a structural sweep bug. The landed mechanisms (all gated tightly and validated zero-regression against the differential harness, §6):

- **Input normalization** (§11.2): `simplifyCollinearRing`, winding normalization, `SplitOverlaps`/`SplitTJunctions` invariants, order-independent crossing-point rounding (`segCanonLess`, overlap endpoints taken from input vertices not re-projected).
- **Shared-vertex / through-vertex crossings.** `handleLocalMin` dispatches `IntersectEdges` at the local minimum. `reconcileSharedVertexCrossings` dispatches at-vertex crossings (a `Touch` on the beam boundary, invisible to `DoIntersections`) by detecting adjacent edges with equal `CurrX` now out of slope order; run after the Tops phase and again after the horizontal flush. `handoffMaxThroughVertex` hands a hot maximum edge's ring onto a **cold** through-edge that continues strictly above the shared vertex (decided from the bound's apex via `boundContinuesAbove`, not the timing-dependent cursor segment).
- **Maxima pairing.** `maximaPartner`/`isMaximaPartner` pair by same-source apex identity (Clipper2 `GetMaximaPair`), scanning the whole AEL so an interleaved confluence (`a-L,b-L,a-R,b-R`) is matched, guarded by an apex-column test. `resolveBetweenMaxima` dispatches each between-edge through `IntersectEdges` before the pair closes. `plateauPartnerPending`/`plateauMaxPartnerPending` defer a plateau maximum to its geometric same-source partner so `DoHorizontal` closes the pair (deferring a cross-source coupling instead mis-times and drops area).
- **Notch-plateau joins (hole∪clip void merges).** `intersectNotchPlateau` (Intersect) and `differenceNotchPlateauJoin` (Difference) handle a hole bound made hot by a "bite" crossing that rides up to the hole apex: the void boundary must continue along the hole's top plateau to its near end, where a cross-source clip ring re-bounds the void, and the two rings join there. Without this the hot bound is dropped and the hole's uncovered region stays filled.
- **Ring closure / same-side maxima.** polyclip's bottom-up sweep, with its mirrored front/back convention, builds rings that meet **same-side** at an apex where Clipper2's top-down sweep closes a ring on itself. `AddLocalMaxPoly` resolves this two ways: a **figure-8 pinch** (emit the apex on each ring, cross-link the two apex `OutPt`s into one self-touching cycle that `splitSelfTouchingRings` later decomposes — no orientation guess) for a genuine interleaving; and a **reverse-one-ring + opposite-side join** when the same-side arrival is a mirror artifact. The two are distinguished by whether a ring was spawned at a crossing (`fromInputMin == false`, the legitimate figure-8) versus two input-minimum rings meeting same-side (the artifact — `fromInputMin`, equal `WindOther`, both-back → reverse+join). The both-back continuing case reverses the spawned ring's sides to restore Clipper2's always-opposite-side invariant.
- **Coincident horizontals.** A coincident different-source horizontal pair does not cross transversally. `dispatchIntersect` returns nil for an **opposite-interior** pair (read from `Segment.Reversed`) — a doubled/cancelling boundary — for non-Xor ops; `processHorzJoins` (`clip/horzjoin.go`) reconnects the skip-separated runs once global topology is known. The skip is suppressed at a boundary **exit** (one bound continues past the overlap), gated by `continuesCollinearHorizontal`, `respawnHandoffAtOverlap`, and an `IsBoundLast` requirement. For Xor the coincident hot edges **interleave** instead (the tunnel branch would collapse a shared plateau apex to a 2-point spike), and Xor is excluded from the horizontal-join pass.
- **Postprocess nesting** (§11.9) and the `traceRing` open-ring guard (returns `ErrOpenRing` rather than following a self-touching sub-cycle to OOM).

**Validation — Monte-Carlo oracle, NOT Clipper2.** Correctness is measured against a Monte-Carlo area oracle and the noise-free set identities (§6). Clipper2 is **not** a usable reference for degenerate small-integer inputs: at native scale it rounds fractional crossings to the integer grid and is itself wrong on all four ops (pre-scaling its input by 1e6 confirms the MC values). On these inputs polyclip's fine fixed-point grid is *more* accurate.

**Refuted approaches (tried, measured, reverted — do not retry):**

- **Local discriminators on the dispatch-skip and the same-side `AddLocalMaxPoly`.** No single *local* predicate (both-continue, hotness, `WindOther`, winding parity) separates touch-vs-overlap or merge-vs-separate at the firing — different coincident crossings *within one op* need opposite decisions. The signals that *do* work are structural: the opposite-interior `Reversed` flag, and the `fromInputMin` spawn provenance for the figure-8 vs reverse-join choice.
- **`WindDx`-derived parity in `AddLocalMinPoly`** (replacing the `outrecIsAscending` proxy): the two do not coincide for legitimately hole-oriented / cross-source-merged rings; regresses badly.
- **Don't-split (whole-horizontal) model** (dropping `SplitOverlaps`' horizontal splitting to match Clipper2): polyclip is built on the no-overlap invariant; reworking winding/ring-construction for whole overlapping horizontals regressed broadly. Keep-split + discriminated skip + join is smaller and sufficient.
- **Clipper2 `splits`/`owner`/`SwapFrontBackSides` machinery is not needed** — it is all `using_polytree_`-gated; polyclip recomputes ownership in postprocess.
