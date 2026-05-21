# polyclip — Polygon Boolean and Offset Library for Go

**Status:** Design draft. No code yet.
**Module path (planned):** `github.com/lestrrat-go/polyclip`
**Audience for this doc:** An implementation agent picking up the project cold. Read top-to-bottom; this is self-contained.

---

## 1. What and why

### 1.1 What

A pure-Go library for 2D polygon operations:

- **Boolean ops** on filled polygonal regions: union, intersection, difference, symmetric difference (XOR).
- **Polygon offset** (a.k.a. "inflate" / "shrink" / "Minkowski sum with disk"): inward and outward, with miter / round / square joins.
- Robust handling of polygons with holes, self-intersections, coincident edges, and overlapping boundaries.

The "shape" primitive is a simple-polygon-with-holes (here called `ExPolygon`) and collections of them (`MultiPolygon`). All ops are closed over `MultiPolygon`.

### 1.2 Why

The downstream consumer is [`lestrrat-go/makislicer`](../makislicer), a 3D-printer slicer. Slicing produces 2D cross-sections per layer; nearly every subsequent quality feature (top/bottom skin detection, bridge detection, gap fill, overhang-aware perimeters, support area calculation, multi-region layers) requires reliable polygon arithmetic. The standard C++ library for this is **Clipper2** by Angus Johnson. The Go ecosystem currently has no equivalent of comparable quality:

- `github.com/akavel/polyclip-go` / `github.com/ctessum/polyclip-go` — older Vatti ports, limited (no offset, fragile on edge cases).
- Various GIS-oriented packages — usually wrap C/C++ via cgo or only handle simple cases.

This library aims to fill that gap with a clean, pure-Go, slicer-grade implementation.

### 1.3 Goals

1. **Correctness** on adversarial input (concentric circles, self-touching polygons, polygons with collinear or coincident edges, near-degenerate slivers).
2. **Pure Go** — no cgo, builds with `go build ./...`, works on every Go-supported platform.
3. **Closed**: every operation takes `MultiPolygon` in and returns `MultiPolygon` out, with no caller-visible "now you have to clean this up" step.
4. **Idiomatic Go API** — small surface, no global state, no inheritance simulation, `error` for caller-recoverable failures.
5. **Acceptable performance**: within 5–10× of Clipper2 on representative slicer workloads (one layer = thousands of segments, sub-millisecond per boolean op). Not a first-pass goal.

### 1.4 Non-goals

- 3D operations. Strictly 2D.
- General CSG / NURBS / Bezier / arcs. Polygons only (line segments).
- Triangulation. Out of scope; a separate library can layer on top.
- Geometric predicates as a public API (point-in-polygon, distance, intersection) — provided where needed but not the focus.
- Drop-in source-compatibility with any existing Go polygon library.
- Cgo bindings to Clipper2. Different library; if someone wants that, they can write it separately.

---

## 2. Module layout

```
github.com/lestrrat-go/polyclip
├── go.mod
├── go.sum
├── README.md
├── DESIGN.md                  (this file)
├── LICENSE                    (MIT, matching lestrrat-go convention)
├── polyclip.go                package doc, top-level types, package-level conveniences
├── point.go                   Point, BBox
├── polygon.go                 Polygon, ExPolygon, MultiPolygon, winding, area
├── boolean.go                 Union, Intersect, Difference, Xor (public API)
├── offset.go                  Offset (public API), JoinType, EndType, OffsetOptions
├── clip/                      internal scanline / boolean engine (subpackage)
│   ├── doc.go
│   ├── segment.go             segment representation, fixed-point coords
│   ├── sweep.go               scanline / event queue
│   ├── intersect.go           segment-segment intersection
│   ├── classify.go            edge winding-count classification
│   └── build.go               output-polygon reassembly
├── fixed/                     internal fixed-point arithmetic helpers
│   ├── doc.go
│   ├── coord.go
│   └── mul.go                 high-precision multiply / determinant for predicates
├── examples/
│   ├── union/main.go
│   └── offset/main.go
└── internal/testdata/
    ├── adversarial/           hand-built tricky cases (TOML or JSON polygons)
    └── golden/                expected outputs for regression
```

Subpackages under `clip/`, `fixed/` are **internal in spirit** but kept exported within the module so tests can address them directly. They are not part of the stable public API; the only stable surface is what's exported by the top-level `polyclip` package.

### 2.1 Why subpackages

The boolean engine is large (~2000 LoC including intersection robustness). Keeping it in a subpackage prevents it from polluting the top-level package's namespace and makes it possible to swap implementations (e.g. add an alternative engine for benchmarking) without changing the public API.

---

## 3. Public API

### 3.1 Core types (`polyclip` top-level package)

```go
// Point is a 2D point. Inputs are float64 in user units (mm, pixels, whatever
// the caller chooses). Internally the engine works in a fixed-point grid.
type Point struct {
    X, Y float64
}

// BBox is an axis-aligned bounding box.
type BBox struct {
    Min, Max Point
}

// Polygon is a simple closed ring of points. The closing edge is implicit
// (p[n-1] → p[0]); do not duplicate the first point at the end.
// By convention outer rings are counter-clockwise and holes are clockwise,
// but the public API accepts either and normalizes on input.
type Polygon []Point

// ExPolygon is one outer ring with zero or more holes nested inside it.
// Holes must be fully contained in Outer and must not overlap each other.
// The library does not enforce this on construction; if you pass invalid
// input, results are undefined. Use Validate() if you need a check.
type ExPolygon struct {
    Outer Polygon
    Holes []Polygon
}

// MultiPolygon is a disjoint union of ExPolygons. All boolean ops return
// MultiPolygon because their result may be one, many, or zero pieces.
type MultiPolygon []ExPolygon
```

### 3.2 Boolean operations (`boolean.go`)

```go
// Union returns a ∪ b.
func Union(a, b MultiPolygon) (MultiPolygon, error)

// Intersect returns a ∩ b.
func Intersect(a, b MultiPolygon) (MultiPolygon, error)

// Difference returns a ∖ b.
func Difference(a, b MultiPolygon) (MultiPolygon, error)

// Xor returns the symmetric difference (a ∪ b) ∖ (a ∩ b).
func Xor(a, b MultiPolygon) (MultiPolygon, error)
```

Variadic convenience for unions of many inputs (useful in the slicer when accumulating skin areas across layers):

```go
// UnionAll returns the union of all inputs. Equivalent to repeated Union but
// O(n log n) instead of O(n²) by pairwise-merging in a tournament.
func UnionAll(polys ...MultiPolygon) (MultiPolygon, error)
```

### 3.3 Offset (`offset.go`)

```go
type JoinType int
const (
    JoinMiter  JoinType = iota // straight extension up to MiterLimit, then chamfered
    JoinRound                  // arc joining the two offset edges (tessellated)
    JoinSquare                 // square join (45° chamfer regardless of corner angle)
)

type EndType int
const (
    // For closed polygons (always closed in this library), only the closed-line
    // end types apply. Open-path offset is currently out of scope.
    EndPolygon EndType = iota // input treated as a closed region; offset operates as Minkowski sum with disk
)

type OffsetOptions struct {
    Join       JoinType // default JoinMiter
    MiterLimit float64  // multiplier on |d| beyond which miters are bevelled. Default 2.0.
    ArcTol     float64  // max chord deviation for round joins, in user units. Default abs(d) * 0.01.
}

// Offset returns the Minkowski sum of m with a disk of radius d when d > 0
// (outward offset), or the Minkowski erosion when d < 0 (inward offset).
// Outer rings and holes are handled together so that, for a CCW outer and
// CW holes, positive d shrinks the printable region — matching the common
// slicer convention. Holes that close up under inward offset are dropped;
// outer rings that vanish are dropped.
func Offset(m MultiPolygon, d float64, opts OffsetOptions) (MultiPolygon, error)
```

### 3.4 Utilities (`polygon.go`, `point.go`)

```go
// SignedArea, Area, IsCCW — standard.
func (p Polygon) SignedArea() float64
func (p Polygon) Area() float64
func (p Polygon) IsCCW() bool

// Reverse flips winding in place.
func (p Polygon) Reverse()

// BoundingBox returns the axis-aligned box containing all points.
func (p Polygon) BoundingBox() BBox
func (e ExPolygon) BoundingBox() BBox
func (m MultiPolygon) BoundingBox() BBox

// Contains reports whether q lies in p (even-odd rule; boundary points are inside).
func (p Polygon) Contains(q Point) bool
func (e ExPolygon) Contains(q Point) bool

// Clean removes consecutive duplicate vertices, collinear interior vertices
// (within tol), and rings smaller than minArea. Returns a new copy.
func (m MultiPolygon) Clean(vertexTol, minArea float64) MultiPolygon

// Validate reports structural problems: self-intersection, holes outside
// outer, overlapping holes, wrong winding. Returns a list of issues (or
// nil); each issue carries enough info for the caller to locate it. Used
// by tests and by callers who want to be defensive about input.
func (m MultiPolygon) Validate() []ValidationIssue

type ValidationIssue struct {
    Kind  IssueKind // SelfIntersecting, HoleOutsideOuter, etc.
    ExIdx int       // which ExPolygon in the MultiPolygon
    Ring  int       // -1 = outer, otherwise hole index
    Msg   string
}
```

### 3.5 Errors

`error` is returned from boolean ops and Offset only for **caller fixable** problems: empty input (debatable — could just return empty), or input that fails internal robustness checks beyond what the engine can repair. Numeric overflow in the fixed-point grid is one example: see §5 for how the engine scales input and what happens when the bounding box is too large.

Validation issues from `Validate()` are not errors; they're a diagnostic.

### 3.6 What's deliberately not in the public API

- Open polylines / polyline offset. (Common request; defer to a follow-up. The slicer needs only closed-region ops.)
- Polygon-to-polyline clipping (clip a path to a polygon). Defer.
- Geometric predicates as a standalone export (point-segment distance, etc.). Defer.
- A streaming API (process polygons one by one without building the full result). The slicer doesn't need it, and it complicates the engine.

---

## 4. Algorithm

### 4.1 Boolean engine: choice

The two practical pure-software choices are:

| Algorithm | Strengths | Weaknesses |
|---|---|---|
| **Vatti** (the algorithm behind Clipper1/Clipper2) | Handles arbitrary polygons including self-intersection and holes; well-documented; survives near-degenerate input with care | Implementation is intricate; ~1500–2000 LoC; numeric robustness needs explicit attention |
| **Martínez-Rueda** | Cleaner conceptual structure than Vatti; smaller codebase | Less battle-tested on adversarial input; published cases of fragility on coincident edges |
| Greiner-Hormann | Very small implementation | Breaks on coincident/overlapping edges; not viable for slicer use |

**Recommendation: Vatti, modeled on Clipper2.** Reference: Angus Johnson's *Clipper2* (`https://github.com/AngusJohnson/Clipper2`), specifically the engine in `CPP/Clipper2Lib/src/clipper.engine.cpp` and the algorithm overview in `CPP/Clipper2Lib/docs/`. The implementation agent should read at least:

1. *A Generic Solution to Polygon Clipping* (Vatti 1992) — the original paper.
2. Clipper2's source as a reference implementation. Note: license is BSL-1.0; **do not copy code**. Use it as an algorithmic reference only. This library is independently implemented under MIT.

### 4.2 Boolean engine: high-level structure

The Vatti algorithm in plain English:

1. **Input prep**: scale floating-point input to a fixed-point integer grid (see §5). Split every polygon into directed edges. Classify each edge as "subject" or "clip" (which input it came from).
2. **Build the event queue**: every edge's lower endpoint (by Y, then X) is a "scanline event". Push all events onto a sorted priority queue.
3. **Scanline sweep**: maintain an "active edge list" (AEL) of edges that cross the current scanline. At each event:
   - **Edge start**: insert into AEL at the correct X position; check for intersections with neighbors and queue them.
   - **Edge end**: remove from AEL; check the neighbors that just became adjacent for intersection.
   - **Intersection**: swap the two edges in the AEL; emit two output vertices.
4. **Edge classification**: each AEL edge carries a "winding count" for its origin (subject or clip). At each output-vertex emission, compute whether the result polygon's interior is to the left or right of the edge — this depends on the operation (union, intersect, etc.) and the two winding counts.
5. **Build output rings**: as edges contribute, glue their endpoints into doubly-linked rings; at the end, walk the rings to produce output polygons, identify which are holes by area sign or by ray-cast.

Implementation specifics, suggested ordering:

- `clip/segment.go` — directed edge type, fixed-point endpoints, source-polygon tag.
- `clip/sweep.go` — event queue and active edge list (skip-list or balanced BST keyed by current-X).
- `clip/intersect.go` — robust segment intersection (see §5.2).
- `clip/classify.go` — winding-count transitions per operation type. Table of `(op, subjWind, clipWind) → emit`.
- `clip/build.go` — rings → output polygons, hole assignment, winding normalization.

### 4.3 Offset engine

Offset walks each input ring once and emits an offset ring directly, vertex by vertex. With `n_i` = right-hand unit normal of input edge `ring[i]→ring[i+1]` and `d` the signed offset distance, each input vertex `v` expands into one or more output vertices based on the local turn direction:

1. Let `a = v + d·prevN`, `c = v + d·nextN` — the offset endpoints of the prev- and next-edges at `v`.
2. Let `cross = prevN × nextN`. The sign of `cross·d` classifies the corner:
   - **Wedge** (`cross·d > 0`): convex offset corner — the two offset edges leave a gap on the offset side. Emit a join (miter apex, square chamfer, or arc-tessellated round), per `OffsetOptions.Join`.
   - **Overlap** (`cross·d ≤ 0`): the two offset edges cross on the offset side. Emit just the miter apex (a single intersection point); for antiparallel adjacent normals, fall back to emitting `a` and `c`.

Holes are offset by `-d` (a CW ring's right-hand normal points into the hole, so the sign flips to keep "outward of the printable region" consistent).

For `d < 0` the apex formula can produce a small "inside-out" ring when `|d|` overshoots the inradius. Detection: every output vertex must satisfy the inward half-plane constraint `(V − ring[i])·n_i ≤ d` for every input edge `i`, with tolerance `max(ArcTol, |d|·1e-6)`. A ring failing any constraint is discarded; if all rings of a piece collapse, the piece is dropped, and if the result is empty `Offset` returns `ErrOffsetEmpty`.

Why direct ring construction over the per-fragment "fat-edge polygons → union" approach (the Clipper2 algorithm sketched in earlier revisions of this doc, and what Clipper2 actually does)?

- **No diff-src coincident-edge pile-up.** The fat-edge approach generates `O(n)` quads that share edges pairwise; running them through repeated `Union` exposes the engine to dense diff-src coincident corners that it does not fuse reliably. Single-ring construction sidesteps the issue.
- **No `O(n²)` pairwise union reduction.** Direct construction is `O(n)`.
- **Exact convex output.** Convex inputs produce the exact closed-form result (no engine round-trip → no fixed-point snapping noise).

The trade-off: for non-convex inputs where inward offset causes partial collapse (U-shape splits into two pieces, deep notch closes), the current implementation doesn't handle topology change — it either accepts the offset ring whole or rejects it whole. Topology change is a follow-up; when implemented it will re-introduce a per-ring boolean self-union to resolve self-intersections in the constructed offset ring.

Implementation lives in `offset.go`:
- `Offset` — public entry; per-piece orchestration, hole sign handling.
- `offsetRing` — walks one ring; produces normals, emits per-vertex points, runs the overshoot validity check for `d < 0`.
- `emitVertex` — wedge-vs-overlap classification and dispatch.
- `appendMiter` / `appendMiterApex` / `appendSquareJoin` / `appendRoundJoin` — per-join geometry.

### 4.4 Algorithmic complexity

- Boolean ops: `O((n + k) log n)` where `n` = total edges and `k` = total intersection points. For typical slicer layers `k = O(n)`, so `O(n log n)`.
- Offset: `O(n)` per ring for construction, plus `O(n·m)` for the inward-overshoot check (m output vertices × n input edges). For typical slicer layers `O(n²)` in the worst case but linear in practice (the check exits early on the first failing vertex).

---

## 5. Numeric robustness

This is the part that separates a working library from a fragile one. **Treat this section as load-bearing**, not optional polish.

### 5.1 Fixed-point internal representation

User input is `float64`. The engine internally scales to `int64` on a uniform grid:

```
internalCoord = int64(round(userCoord * Scale))
```

`Scale` is chosen per-operation based on the input bounding box. The constraint is that all intermediate products (segment-segment intersection determinants are up to degree-4 in coordinates) fit in `int64` (or in `int128` synthesized from two `int64`s for the high-precision determinant). A simple rule:

- Bounding-box span ≤ 2²⁰ user units after centering → `Scale = 2⁴⁰`. Total coordinate range ≤ 2⁶⁰. Intersection determinants stay below 2¹²⁰ — fits comfortably in `int128`.
- For larger bounding boxes, reduce `Scale` proportionally. Document the precision implications.

After the engine produces output, descale back to `float64` for the caller.

**Why fixed-point at all?** Vatti's scanline is fragile against repeated near-degenerate intersection coordinates floating around in `float64`. Integer coordinates eliminate "same point computed two ways gives slightly different float64s, breaks topology" — the most common source of Vatti bugs. Clipper does this; we should too.

### 5.2 Robust segment intersection

Two segments `A→B` and `C→D` intersect when the signs of four 2D cross products differ appropriately. The cross products are determinants:

```
det(P, Q, R) = (Q.X - P.X) * (R.Y - P.Y) - (Q.Y - P.Y) * (R.X - P.X)
```

In fixed-point with coordinates up to 2⁶⁰, this product is up to 2¹²⁰. Use a small `int128` helper (`fixed/mul.go`) for the multiplication and sign. **Never** compute these signs in `float64`.

The intersection *point*, by contrast, requires division and can be computed in `float64` (or in `int64` rounded to the grid) — only the orientation predicates need full precision.

References: Shewchuk's paper *Adaptive Precision Floating-Point Arithmetic and Fast Robust Geometric Predicates* (1997). We don't need the full adaptive technique because `int64` gives us exact arithmetic for orientation; this is the simpler "exact integer predicates" approach.

### 5.3 Coincident edges

When two input edges lie on the same line (overlapping or touching), naive Vatti produces zero-area output rings. Clipper2 handles this by detecting collinear edges during AEL maintenance and merging them. The implementation agent should:

- Treat coincident edges as a first-class event in the sweep.
- Document the rule for "what does the union of two coincident edges contribute" with reference to the standard winding-number interpretation.

### 5.4 Vertices on edges (T-junctions)

When a vertex of one polygon lies exactly on an edge of another, the integer grid makes this exactly representable, so there's no ambiguity. The engine should:

- Detect during the sweep when a new event's point lies on an active edge.
- Split that edge at the new point and treat the result as two events.

This is the single most common source of Vatti bugs in implementations that didn't get §5.1 right.

### 5.5 Self-intersecting input

The library accepts self-intersecting input rings. The engine handles them naturally because Vatti's intersection step processes every crossing, including self-crossings. Output is always simple (non-self-intersecting).

---

## 6. Testing strategy

### 6.1 Unit tests

Standard `_test.go` files alongside each source file. Required coverage:

- `point_test.go` / `polygon_test.go` — accessors, predicates, area/winding/contains.
- `clip/intersect_test.go` — orientation predicates against hand-computed integer inputs; segment-segment intersection cases (cross, touch, collinear-overlapping, collinear-disjoint, parallel).
- `clip/sweep_test.go` — event queue ordering, AEL insertion/removal, intersection detection. Use small hand-built scenes (2–6 segments).
- `clip/build_test.go` — ring reassembly from a fixed list of contributions.
- `offset_test.go` — per-join geometry (miter / square / round), inward/outward, collapse detection, round-trip area tolerance.

### 6.2 Integration tests (golden)

In `internal/testdata/`:

- **Hand-built adversarial cases** as TOML (or JSON, agent's call). Each test case: input polygons + operation + expected output polygons. Cases:
  - Two squares overlapping → union, intersect, difference.
  - Square minus square = annulus (hole appears).
  - Two squares touching along an edge → union.
  - Two squares touching at a single vertex → union.
  - Self-touching "8" → union with another polygon.
  - Concentric circles (sampled) of radii 10 and 5 → annulus.
  - Star (5-pointed, self-intersecting if filled "evenodd") → union with self.
  - Large vs. tiny polygon (precision stress).
- **Random fuzz**: generate random polygons (random walks closed back to start, with optional rejection-sample to avoid self-intersection for some tests). For each, check invariants:
  - `Union(A, A) == A`
  - `Intersect(A, A) == A`
  - `Difference(A, A) == ∅`
  - `Xor(A, B) == Union(Difference(A,B), Difference(B,A))`
  - `Area(Union(A,B)) == Area(A) + Area(B) - Area(Intersect(A,B))`
- **Offset round-trip**: `Offset(Offset(A, +d), -d) ≈ A` for small `d` and convex `A` (not exact for non-convex; tolerance per-test).
- **Real slicer slices**: take a layer's output from makislicer's STL test fixtures, run the four boolean ops against itself and against scaled/translated copies, regression-check output bounding box and area.

### 6.3 Property tests

Use `testing.F` (Go 1.18+ native fuzzing) to feed random polygons into the boolean engine and assert the invariants in §6.2. Fuzz corpus seeded with the adversarial hand-built cases.

### 6.4 Benchmarks

- `BenchmarkUnion_NxN` — union of N×N grid of overlapping squares for N ∈ {10, 100, 1000}.
- `BenchmarkOffset_LayerSlice` — offset a representative slicer layer (~200 vertices) by ±0.4 mm.
- Compare against a recorded baseline; CI flags regressions over 20%.

---

## 7. Phased implementation plan

The phases are designed so each one produces a usable, testable artifact even if subsequent phases never happen. Estimated LoC is for the agent to calibrate, not a hard budget.

### Phase 0 — Skeleton (≤ 200 LoC) — **DONE**
- [x] `go mod init github.com/lestrrat-go/polyclip`
- [x] `polyclip.go`, `point.go`, `polygon.go` — public types, area/winding/bbox/contains, no boolean ops yet.
- [x] `LICENSE` (MIT), `README.md` (one-paragraph stub pointing to DESIGN.md).
- [x] CI scaffold (`.github/workflows/ci.yml`) running `go test ./...` and `go vet`.

### Phase 1 — Fixed-point core (≤ 500 LoC) — **DONE**
- [x] `fixed/coord.go` — scaled `int64` coordinate type, scale-from-bbox helper.
- [x] `fixed/mul.go` — `int128` multiply for orientation predicates.
- [x] Tests for predicates against hand-computed integer expectations.

### Phase 2 — Boolean engine: union only (≤ 1500 LoC) — **DONE**
- [x] `clip/segment.go`, `clip/sweep.go`, `clip/intersect.go`, `clip/classify.go` (union table only), `clip/build.go`.
- [x] Public `Union(a, b MultiPolygon)`.
- [x] All adversarial-case tests for union pass.
- [x] Fuzz seed corpus committed.

### Phase 3 — Other boolean ops (≤ 200 LoC delta) — **DONE**
- [x] Extend the classification table in `clip/classify.go` for intersect, difference, xor.
- [x] Public `Intersect`, `Difference`, `Xor`.
- [x] Invariant tests from §6.2 pass.

### Phase 4 — Offset (≤ 800 LoC) — **DONE**
- [x] `offset.go` — direct per-ring construction, no boolean self-union (topology-change cases deferred).
- [x] Miter and round joins (plus square).
- [x] Round-trip property tests.

### Phase 5 — Quality & speed
- [x] `UnionAll` tournament reduction for `O(n log n)` Union calls instead of repeated pairwise.
- [x] `Clean()` implementation — duplicate / collinear vertex removal, small-ring/hole drop.
- [x] `Validate()` implementation — winding / self-intersection / hole containment / hole overlap.
- [ ] Benchmarks; profile and optimise hot paths in the sweep.
- [ ] Documentation pass: every public symbol has a Go doc comment with at least one example.

### Phase 6 — Examples and v0.1 release
- [x] `examples/union/`, `examples/offset/` runnable programs.
- [ ] Downstream `lestrrat-go/makislicer` switches off its naive offsetter onto polyclip.

---

## 8. Constraints, conventions, and gotchas for the implementing agent

### 8.1 Go version

Target Go 1.22 minimum (for native fuzzing maturity and the `slices` / `cmp` packages). Don't use generics where a non-generic version is just as readable; the engine works in concrete types (`int64`, etc.).

### 8.2 Dependencies

**Zero external module dependencies.** The library should be `go.mod`-clean with only the standard library. This is non-negotiable — the whole point is to be a leaf dependency that downstream slicers can pull in without bringing the world.

### 8.3 Concurrency

The public API is **safe for concurrent use across goroutines on different inputs**. Individual `MultiPolygon` values are not synchronized — callers are expected not to mutate one while another goroutine reads it (the same rule as `[]int`).

Internal parallelism is **not** in scope for v0.1. The slicer parallelizes at the layer level above us.

### 8.4 Style

- Follow `gofmt`, `go vet`, `staticcheck`. CI enforces.
- Errors wrap with `fmt.Errorf("polyclip: ...: %w", ...)`.
- Public functions have doc comments starting with the function name, per Go convention.
- Internal subpackages (`clip/`, `fixed/`) may use shorter doc comments and may export aggressively for testing — they're not part of the stable API.
- No package-global mutable state. No `init()` that does work.

### 8.5 Things that will look tempting but are wrong

- **"Just use `float64` everywhere; modern CPUs are fast"** — yes they are, but Vatti will produce topologically broken output. See §5.1. Take this seriously.
- **"Just copy Clipper2's source and translate it"** — license incompatibility (BSL-1.0 → MIT). Use as algorithmic reference, write from scratch.
- **"Greiner-Hormann is simpler; let's start there"** — it can't handle coincident edges. The slicer's input frequently has coincident edges (perimeter offsets that just touch, layer contours from CAD with shared edges). Don't.
- **"Offset can be done without a boolean engine via miter math"** — that's what makislicer's current naive offsetter does and it's why we're building this library. See §4.3.

### 8.6 When to ask the human

The implementing agent should ping the human (open an issue / ask in a follow-up) when:

- Phase 1 (`fixed/`) is done but you're unsure between two coordinate-scaling strategies — get sign-off before committing to a `Scale` policy.
- Phase 2 union is producing topologically correct but visually different output from Clipper2 on a specific case — verify it's still "correct" before treating it as a bug.
- You hit a robustness case not covered in §5 — document it and ask whether to handle or to declare out-of-scope.

Otherwise, follow the design. If a deviation feels necessary, write it up in this doc (add a "Deviations" section) before coding.

---

## 9. References

1. Vatti, B. R. (1992). *A Generic Solution to Polygon Clipping*. Communications of the ACM, 35(7), 56-63.
2. Johnson, A. *Clipper2*. https://github.com/AngusJohnson/Clipper2 — algorithmic reference, BSL-1.0; do not copy code.
3. Shewchuk, J. R. (1997). *Adaptive Precision Floating-Point Arithmetic and Fast Robust Geometric Predicates*. Discrete & Computational Geometry, 18(3), 305-363.
4. Martínez, F., Rueda, A. J., & Feito, F. R. (2009). *A new algorithm for computing Boolean operations on polygons*. Computers & Geosciences, 35(6), 1177-1185. (Reference for the rejected alternative.)
5. OrcaSlicer source, `src/libslic3r/ClipperUtils.{cpp,hpp}` and `deps/Clipper2/` — shows how a real slicer wires Clipper2 into its pipeline.

---

## 10. Status of this document

Living document. The implementing agent is expected to update it as decisions are made. Each phase's PR should include a sentence in §7 marking the phase done, plus any deviations or refinements.

Next action: an agent reads this doc end to end, confirms understanding (or asks clarifying questions), and starts on Phase 0.

---

## 11. Phase 2 implementation notes

This section nails down the design decisions that §4.2 left implicit, so the sweep / classify / build pieces can be implemented without rediscovering them. Read after §4.2 and §5 — those still hold.

Phase 0, Phase 1, and Phase 2 increments 1–3 are already in place (`fixed/`, `clip/segment.go`, `clip/intersect.go`, `clip/event.go`, `clip/ael.go`). What remains is the sweep loop itself, the classification, the ring assembly, and the public `Union`.

### 11.1 What "the engine" consumes and produces

```
MultiPolygon (subject, float64)
MultiPolygon (clip, float64)
        │
        ▼  preprocess
[]Segment (fixed-point, source-tagged, deduped, with overlapping pairs split at overlap endpoints)
        │
        ▼  sweep
sequence of "output contributions" — per contributing AEL edge a polyline of (point, dir) records
        │
        ▼  build
[]OutputRing (doubly-linked, closed)
        │
        ▼  postprocess
MultiPolygon (fixed-point) → MultiPolygon (float64)
```

Each stage has its own file; no stage knows the next stage's internals.

### 11.2 Preprocess

1. Compute the union bbox of subject and clip in user space.
2. Build a single `fixed.Scale` from that bbox.
3. For each ring (subject outer/holes, clip outer/holes) iterate edges; call `clip.NewSegment(snap(a), snap(b), src)` per edge.
4. Drop degenerate segments (`Segment.Degenerate()`).
5. **Overlap split.** Run `clip.Intersect` on every pair where the segments are exactly collinear (cheap pre-pass: sort by line equation hash). For any `CollinearOverlap` result, replace both segments with three pieces each so afterwards the only collinear pairs are *fully* coincident, not partially overlapping. This means the main sweep never sees `CollinearOverlap`.
6. Push `EventBot{seg}` and `EventTop{seg}` for each surviving non-horizontal segment. For each surviving horizontal segment push `EventHoriz{seg}` (see §11.8).

Coincident-edge handling proper is in §11.7; horizontal-segment handling is in §11.8.

### 11.3 Sweep state — what an ActiveEdge tracks

```go
type ActiveEdge struct {
    Seg   *Segment
    CurrX fixed.Coord

    // Vatti winding bookkeeping. Computed at insertion and updated on
    // every intersection swap.
    WindSelf  int  // signed winding count for Seg.Src up to and INCLUDING this edge
    WindOther int  // signed winding count for the OTHER source up to this edge (exclusive)

    // Output linkage. Non-nil only for edges classified as contributing.
    Out *OutPt
}
```

Signed counts: an edge contributes `+1` if traversing it left-to-right takes you from outside to inside its source ring, `-1` otherwise. `Reversed` segments contribute the opposite sign of non-reversed segments.

Computation at insertion:
- Find the left neighbour `L` in the AEL.
- `WindSelf = L.WindSelf (if L.Src == this.Src else L's same-source predecessor's WindSelf) + this edge's signed contribution`.
- `WindOther = L.WindSelf (if L.Src != this.Src else L's other-source predecessor's WindSelf, or 0 if none)`.

There's room to be cleverer but the above is the correct fallback.

### 11.4 Classification table

The boundary-flip rule: define `inside(side) = inside_subject(side) OP inside_clip(side)` where `OP` is the boolean op. An ActiveEdge contributes to output iff `inside(left of edge) != inside(right of edge)`.

For an AEL edge of source `S` we have winding counts on both sides:

- Left side: `wS = WindSelf - delta`, `wO = WindOther`, where `delta` is the edge's signed contribution (±1).
- Right side: `wS' = WindSelf`, `wO' = WindOther`.

The other source's count is identical on both sides (only S's edges flip it). So the contribution rule reduces to:

| Op         | Contributes iff                                                                 |
|------------|---------------------------------------------------------------------------------|
| Union      | `wO == 0` AND `(WindSelf == 0) != (WindSelf-delta == 0)`                        |
| Intersect  | `wO != 0` AND `(WindSelf == 0) != (WindSelf-delta == 0)`                        |
| Difference | (S subject) `wO == 0` AND `(WindSelf == 0) != (WindSelf-delta == 0)`; (S clip) `wO != 0` AND same flip on subject side |
| Xor        | `(WindSelf == 0) != (WindSelf-delta == 0)` (every flip contributes)             |

For **non-self-intersecting** input, `WindSelf` only ever toggles between 0 and ±1, so the flip predicate is always true and the rule simplifies to just the `wO` clause. Phase 2 first cut may assume this; Phase 5 lifts the simplification for self-intersecting input.

Only the Union row is needed for Phase 2. Phase 3 fills in the rest.

### 11.5 Event-handler procedures

**EventBot(seg)** — segment starts:
1. Build `ae` with `CurrX = seg.Bot.X`.
2. `i := AEL.Insert(ae)`.
3. Compute `ae.WindSelf`, `ae.WindOther` from `AEL.LeftOf(i)`.
4. Run classification → set `ae.Contributing`.
5. If contributing: emit a new `OutPt` linked to `ae.Out`.
6. Schedule intersection checks: `(AEL.LeftOf(i), ae)` and `(ae, AEL.RightOf(i))`.

**EventTop(seg)** — segment ends:
1. Find `ae` for `seg` in AEL (engine keeps a map from `*Segment` to `*ActiveEdge`).
2. If contributing: emit a final `OutPt` at `seg.Top`, close out the output ring (or hand it to the next adjacent contributor for stitching, see §11.6).
3. `i := AEL.IndexOf(ae); AEL.Remove(ae)`.
4. The two edges that just became adjacent (left + right of `i`) may now cross — schedule a check.

**EventIntersection(segA, segB, p)** — neighbours cross:
1. Locate both in the AEL. They must currently be adjacent (otherwise this event was scheduled but the configuration changed; discard).
2. For each contributing edge, emit an `OutPt` at `p`. The two contributing rings (if any) may need to be **stitched** here — this is the only place two distinct rings merge into one.
3. `AEL.SwapAt(i)`. Update `WindSelf` and `WindOther` for both (they swap their relative positions, so each one's predecessor changed).
4. Re-classify both. A non-contributing edge can become contributing or vice versa across a crossing.
5. Schedule fresh intersection checks for the new neighbours.

### 11.6 Output ring data structure

```go
type OutPt struct {
    P     fixed.Point
    Next  *OutPt
    Prev  *OutPt
}

type OutRing struct {
    // Doubly-linked cycle of points. nil only for closed-and-released rings
    // that have been moved into the result.
    Pt  *OutPt
    IsHole bool   // determined after sweep by signed area
}
```

Stitching at intersection events is the operation most likely to be wrong:
- Two contributing edges may belong to the **same** OutRing (they're the two sides of a feature) → no stitch, just emit one new OutPt at `p` on each side's chain.
- Two contributing edges may belong to **different** OutRings (two features just met) → stitch: splice the two chains into one cycle, retain one OutRing, mark the other absorbed.
- A contributing edge meeting a non-contributing edge → emit the new OutPt on the contributing side only.

Implementation: every ActiveEdge holds `Out *OutPt` pointing at the most recently emitted vertex on that edge's chain. Stitch by relinking `Prev`/`Next` between the two chain tails.

### 11.7 Coincident edges (fully overlapping after preprocess split)

After §11.2 step 5, the only collinear pairs the sweep sees are pairs that share `Bot` AND `Top`. These behave as follows in Union:

- Same source, same direction (duplicate input edge) — dedup in preprocess.
- Same source, opposite direction — they cancel; drop both.
- Different sources, same direction — they double-count. Output emits **one** edge with the union of their winding contributions.
- Different sources, opposite direction — they cancel; output emits zero contribution at this position.

**Implementation**:

- Same-source cases are handled in preprocess by `DedupCoincidentEdges` in `clip/preprocess.go`: same-direction duplicates dropped, opposite-direction pairs cancelled.

- Different-source cases occur most naturally with axis-aligned overlapping inputs (e.g. two overlapping axial rectangles where the top and bottom edges' middle pieces share `Bot`/`Top` after `SplitOverlaps`). The bound model places each source's leading horizontal in a separate bound, so a sorted-insert of the AEs misses the topological intersection that should occur where the two bounds' horizontals cross another bound's endpoint. Two synthetic-intersection passes recover the correct ring topology:

  1. **At local-min spawn** (`sweep.processSynthIntersectsAtLocalMin`): after spawning both new bounds, walk AEs trapped between leftAE and rightAE in the AEL. For each whose `Seg.Bot.Y == lm.Vertex.Y` AND whose `Seg.Bot.X` matches one of the new bound's leading-horizontal endpoints, run `synthIntersect` — `IntersectEdges`' dispatch logic without an AEL swap (our sorted-insert path already has the bounds at their final positions; only the ring-surgery side effects are needed). For diff-src same-dir at the bottom of two overlapping axial squares, this swaps the existing ring's FrontEdge from the first square's right side to the second square's right side.

  2. **At local-max** (`sweep.findSynthMaxPartner` in `closeBound`): symmetric. If `maxPt` is an INTERIOR vertex (not a local-min or local-max) of an AEL neighbour's bound, that neighbour is a synth-partner — perform `synthIntersect` to swap ring ownership before retiring the closing edge. This catches the top-of-overlap coincident pair, swapping the ring's BackEdge so the ring continues through the other source's bound to its actual local max.

  3. `closeBound`'s Case C is also relaxed from `Contributing && IsHotEdge` to just `IsHotEdge`. After a synth-intersect, the new owner of a chain may be classified non-contributing at the local-max scanline but is still hot — its closure must run so the ring closes cleanly.

  **Scope limitation**: the synth-intersect mechanism is currently Union-specific. For Intersect / Difference / Xor on axially-overlapping inputs (the case where two rectangles share a horizontal coincident edge that gets dropped/split by preprocess), the engine produces incorrect rings. Diamonds and other non-axial overlaps work for all four ops because they cross via real `IntersectEdges` events. Op-aware synth-intersect (different swap semantics per op) is a planned follow-up. `TestIntersect/Difference/XorOverlappingAxisAligned` in `boolean_adversarial_test.go` are skipped pending this work.

  Identical inputs (`Union(A, A)` and analogues) remain a degenerate case — every edge becomes a diff-src coincident pair at the SAME local-min vertex, which the bound model's BuildLocalMinima can't disambiguate. These are short-circuited at the API level in `boolean.go` (`mpolyEqual` check).

### 11.8 Horizontal segments

A horizontal segment `h` has `Bot.Y == Top.Y` and lives at a single scanline `Y_h`. It contributes zero to `WindSelf` over any non-zero Y interval, so it never enters the AEL; instead, the engine processes horizontals via a dedicated **horizontal pass** at each scanline.

Event-queue contract for horizontals (changes to §11.5):

- A horizontal segment generates a single `EventHoriz` event at `(Y_h, Bot.X)` carrying the full segment.
- `EventKind` ordering at the same `(Y, X)` is: `Top < Bot < Horiz < Intersection`. Closing edges leave the AEL first; new edges enter next; horizontals walk through the resulting AEL last (so a horizontal local minimum can see the two ascending bounds it bridges). See §12.6 for the rationale — this ordering is what Clipper2's `PushHorz`/`PopHorz` pair implements, and it supersedes an earlier draft of this section that placed `Horiz` between `Top` and `Bot`. The current `clip/event.go` `EventKind` enum still reflects the older ordering and must be corrected as part of the increment that wires `DoHorizontal`.

Horizontal pass procedure (executed when `EventHoriz` fires):

1. The horizontal's two endpoints are vertices of the input. Each endpoint also belongs to an adjacent non-horizontal segment of the same ring; those non-horizontals were already inserted (left endpoint) or are about to be inserted (right endpoint) into the AEL at this Y.
2. Walk the AEL left-to-right from `min(Bot.X, Top.X)` to `max(Bot.X, Top.X)`. For each AEL edge `e` with `e.CurrX` in that range:
   - If `e` is contributing **and** `h` is contributing (per the classification table — `h` has its own `WindSelf` and `WindOther` computed from its position in the segment list relative to other horizontals at this Y), emit an output point at `(e.CurrX, Y_h)` on `e.Out`'s chain.
   - This stitches `h` into the contributing output rings as a horizontal segment between consecutive stitch points.
3. If both endpoints of `h` are themselves stitch points (the common case — a horizontal connects two non-horizontal edges of the same ring), the horizontal's two endpoints close out the local minima / maxima of those two adjacent non-horizontal edges.

Classification for horizontals:

Treat `h` as if it were "swept" at `Y_h + ε`. Its `WindOther` equals the running winding count of the other source at `(Bot.X, Y_h)`. Its `WindSelf` equals the running self-count at `(Bot.X, Y_h)` plus its own ±1 contribution. From there the classification table in §11.4 applies unchanged.

Coincident horizontals (two collinear horizontals at the same `Y_h` with overlapping X ranges) are handled by the preprocess overlap split in §11.2 — the sweep never sees partially overlapping horizontals.

Implementation hint: store horizontals separately from non-horizontals during preprocess and emit `EventHoriz` events keyed only on `Y_h`. The horizontal pass then sees all horizontals at this scanline together, processes them in `Bot.X` order, and synchronises with the rest of the sweep via the `EventKind` ordering above.

### 11.9 Postprocess (clip/build.go)

After the sweep terminates, every released `OutRing` becomes a `Polygon`:

1. Walk the cycle starting from `OutRing.Pt`, dedup consecutive equal points, output a `Polygon`.
2. Sign of `Polygon.SignedArea` determines `IsHole`: positive → outer (CCW), negative → hole (CW), then `Polygon.Reverse()` so all outputs follow the convention (CCW outer, CW holes).
3. **Hole assignment**: for each hole, find the smallest outer that contains a sampled vertex of the hole. Use bbox prefilter then `Polygon.Contains` (already implemented in §3.4).
4. Build `[]ExPolygon` grouping each outer with its assigned holes.
5. Wrap as `MultiPolygon`.
6. Unsnap fixed-point coordinates back to float64.

### 11.10 Invariants the engine must maintain

Invariants 1, 2, and 5 below are checked as post-conditions by `clip.CheckInvariants` (called from `clip/invariants_test.go`). Invariants 3 and 4 are aspirational under the §11.7 synth-intersect implementation and are revised:

1. **AEL ordering**: after each event the AEL is sorted left-to-right by `CurrX` with slope tie-break. Checked by `clip.CheckAELSorted` (runtime callable; tests on AEL snapshots).
2. **WindSelf bounded**: no `WindSelf` exceeds the number of input rings of that source. Checked as a sanity proxy: closed-ring count ≤ input segment count.
3. ~~Every `ActiveEdge` with non-nil `Outrec` is classified contributing.~~ **No longer holds**: §11.7's `synthIntersect` legitimately leaves a hot edge non-contributing in mid-sweep (e.g. an edge swapped into another ring whose interior classification doesn't match this edge's own boundary status). Replaced by: **every closed output ring's cycle is well-formed** (Next/Prev links round-trip, OutPt back-pointers consistent). Checked by `clip.CheckInvariants`.
4. ~~No two contributing same-source edges are adjacent in the AEL.~~ **Transiently violated**: during synth-intersect passes at local-min and local-max, adjacency may temporarily include two same-source contributing edges before re-classification settles. Not checked.
5. **Rings either close or retire**: at sweep end, every `OutRec.Pts` is either non-nil (closed cycle) or nil (retired via `JoinOutrecPaths`). No partially-open rings. Checked by `clip.CheckInvariants`.

### 11.11 Implementation order for the remaining Phase 2 increments

Recommended commit boundaries from here:

- **Increment 4:** preprocess pipeline (`clip/preprocess.go`): scale, snap, drop degenerates, overlap split, coincident-edge dedup. Tests cover each transformation in isolation. No sweep yet — the function returns `[]Segment`.
- **Increment 5:** sweep loop skeleton (`clip/sweep.go`): event loop that processes Bot/Top/Intersection without yet computing classification or emitting output. Logs/records the sequence of events as an internal trace. Test asserts the trace for a couple of hand-built inputs.
- **Increment 6:** winding bookkeeping + classification table for Union (`clip/classify.go`). At this point `Contributing` is set correctly on every AEL edge; still no output emission. Test the trace including contribution flags.
- **Increment 7:** output ring assembly (`clip/build.go`) plus public `Union` (`boolean.go`). End-to-end works for the simplest adversarial cases.
- **Increment 8:** rest of the adversarial suite (concentric rings, coincident edges, self-touching 8s, T-junctions), fuzz seed corpus.

Each increment ships with its own tests; each is its own commit on the feature branch. Phase 2 fast-forwards to main when increment 8's adversarial suite passes.

---

## 12. Algorithm reference notes (from Clipper2)

Clipper2 is BSL-1.0; we use it as algorithmic reference only and write Go from scratch under MIT. This section distills the parts of `CPP/Clipper2Lib/src/clipper.engine.cpp` that the Phase 2 implementer needs to translate behavior from. It assumes Clipper2 is checked out at `../../AngusJohnson/Clipper2/`; file:line references in this section point into that tree.

### 12.1 "Bound" terminology and the local-minima preprocessing

Clipper2 reframes each input polygon as a sequence of alternating **ascending** and **descending bounds** (`engine.cpp:28-32`). A bound is a chain of input edges traversed monotonically in Y. A **local minimum** is the vertex where a descending bound meets an ascending bound — the polygon "turns from going down to going up." A **local maximum** is the inverse — both bounds going up meet at a top vertex.

This reframing is significant because the §11 model treats every input edge as its own scanline event, while Clipper2 treats each *bound* as a single AEL entry that updates through multiple input edges via `UpdateEdgeIntoAEL`. For polyclip we have a choice:

- **Stay with per-edge events** (§11): simpler events; redundantly emits Bot/Top at every non-extremum vertex, which the engine must handle as "edge continues into next edge" rather than "ring closes."
- **Move to bounds**: pre-pass identifies local minima; each minimum spawns two AEL entries that each represent an entire ascending or descending bound; Top events fire only at local maxima.

**Recommendation:** Move to bounds before increment 7. The §11 per-edge model is workable but adds complexity at every non-extremum vertex that the bound model eliminates. The pre-pass is small: walk each input ring, identify each Y-direction reversal, and emit a `LocalMinima` record carrying pointers to the two emerging bounds.

Bound representation in Clipper2 (`engine.h:104-127`): an `Active` carries a pointer to its current input edge and a pointer to `LocalMinima` for the bound's origin. `UpdateEdgeIntoAEL` (`engine.cpp` near 1772) advances the pointer to the next edge in the bound when a sub-edge's Top is reached without ending the bound.

### 12.2 AEL entry — required fields

Clipper2's `Active` (`engine.h:104-127`) and `OutRec` (`engine.h:79-100`) imply the per-edge state we need:

- `Seg *Segment` (or in the bound model, `LocalMin *LocalMinima` plus a current-edge cursor).
- `WindCount int` — winding count of this edge's source up to and including this edge (Clipper2's `wind_cnt`).
- `WindCount2 int` — winding count of the *other* source (Clipper2's `wind_cnt2`).
- `WindDx int` — signed input direction of the edge, ±1. Used to update `WindCount` of neighbours when this edge crosses theirs. Same idea as our `signedContribution`.
- `Outrec *OutRec` — non-nil iff this edge is currently a "hot" edge contributing to a ring (Clipper2's `IsHotEdge` ≡ `outrec != nullptr`).

`OutRec` carries `front_edge` and `back_edge` — the two AEL edges currently building this ring. An edge is the **front** of its OutRec iff it's the leftmost contributor; the **back** iff rightmost. New points emitted on the front side prepend to `OutRec.pts`; new points on the back side append. This is the part §11.6 missed.

`Active.IsFront()` is then a derived predicate: true iff `outrec.front_edge == this`. Many decisions in `IntersectEdges` and `AddLocalMaxPoly` branch on `IsFront`.

### 12.3 AddLocalMinPoly (`engine.cpp:1332-1377`)

Called when two AEL edges become the two sides of a brand-new contributing ring. Inputs: `e1, e2 *Active`, the local minimum point `pt`, and an `is_new` flag.

Algorithm:

1. Allocate a new `OutRec`.
2. Assign `e1.outrec = e2.outrec = outrec`.
3. Decide which edge is `outrec.front_edge` (left side) and which is `back_edge` (right side). The choice depends on the nearest prior "hot" edge in the AEL and on `is_new`:
   - If no prior hot edge: `is_new ? (front=e1, back=e2) : (front=e2, back=e1)`.
   - If prior hot edge with `OutrecIsAscending(prev) == is_new`: swap (front=e2, back=e1).
   - Else: (front=e1, back=e2).
4. Create the ring's first `OutPt` at `pt` and set `outrec.pts = op`.

`is_new` is true when the local minimum is a real input vertex; false when it's a "synthetic" minimum created by IntersectEdges' "AddLocalMaxPoly + AddLocalMinPoly" tunnel case (§12.5).

**Convention note**: polyclip's `AddLocalMinPoly` and `handleLocalMin` are CALLER-SIDE INVERTED relative to Clipper2's "front = leftmost" rule above. Our callers pass `(rightAE, leftAE)` as `(e1, e2)`, which combined with the §12.3 step-3 decision produces `outrec.FrontEdge = rightAE` (= rightmost) for the common no-prior-hot, `is_new=true` case. Everything downstream stays internally consistent because the FrontEdge/BackEdge identity is what matters (not which side is geometrically left), and `Polygon.SignedArea` in postprocess (§11.9) determines orientation independently. `handleHorizMin` follows the same convention. Keep this inversion in mind when comparing engine.cpp traces side-by-side.

### 12.4 AddLocalMaxPoly (`engine.cpp:1380-1433`) and JoinOutrecPaths (1435+)

Called when two AEL edges meeting at a local maximum together close (or merge) their ring(s).

Algorithm:

1. If either edge has `IsJoined` set, call `Split` first (handles certain mid-sweep ring splits — not load-bearing for first cut).
2. Sanity check: `e1.IsFront() != e2.IsFront()`. If equal, the sides have crossed — error or open-end fix. For first cut: assert and bail.
3. Add an OutPt at `pt` on `e1`'s chain.
4. **If `e1.outrec == e2.outrec`** (both edges belong to the same ring): the ring closes. Uncouple both edges from the OutRec; the ring is now a complete cycle. This is the normal case.
5. **Else** (two different rings meeting): the two rings merge into one via `JoinOutrecPaths(e1, e2)` (with a chosen order based on `e1.outrec.idx < e2.outrec.idx`).

`JoinOutrecPaths` splices the two doubly-linked OutPt chains into one cyclic chain and discards the second OutRec.

### 12.5 IntersectEdges decision tree — closed paths (`engine.cpp:1772+`, closed-path branch starts ~1866)

This is the heart of the engine. After updating winding counts for the swap (the swap re-orders the two edges; their `wind_cnt`s exchange roles), the function dispatches based on the *prior* winding counts and the contributing/hot status of each edge.

Let `e1Hot`, `e2Hot` = `IsHotEdge` of each edge before the intersection.
Let `w1`, `w2` = old `wind_cnt` of each edge (signed for NonZero/Positive/Negative rules; abs'd for EvenOdd).
Let `w1c2`, `w2c2` = old `wind_cnt2` of each edge.

**Branch A — both hot** (lines 1941-1985):
- If `(w1 ∉ {0,1}) || (w2 ∉ {0,1}) || (different polytype && op != Xor)`: call **AddLocalMaxPoly(e1, e2, pt)** — close both rings together. This is the "two rings end at the intersection" case.
- Else if `e1.IsFront() || e1.outrec == e2.outrec`: call **AddLocalMaxPoly + AddLocalMinPoly** — tunnel case where two rings touch at this point and split into two new ones. `AddLocalMinPoly` here uses `is_new=false` because the minimum is synthetic.
- Else: call **AddOutPt** on each edge and `SwapOutrecs(e1, e2)` — the two rings interleave (each contribute a vertex here and swap which OutRec they're associated with going forward).

**Branch B — exactly one hot** (lines 1986-2005):
- Call **AddOutPt** on the hot edge, then `SwapOutrecs(e1, e2)` — the cold edge effectively inherits the hot edge's OutRec going forward (and vice versa).

**Branch C — neither hot** (lines 2006-2085):
- If different polytype: call **AddLocalMinPoly(e1, e2, pt, false)** — the intersection creates a new ring.
- Else (same polytype) and `w1 == 1 && w2 == 1`: dispatch by op:
  - `Union`: if `w1c2 <= 0 && w2c2 <= 0`: **AddLocalMinPoly**.
  - `Difference`: if (clip and both `wc2 > 0`) or (subject and both `wc2 <= 0`): **AddLocalMinPoly**.
  - `Xor`: always **AddLocalMinPoly**.
  - `Intersect` (default): if `w1c2 > 0 && w2c2 > 0`: **AddLocalMinPoly**.

For our Phase 2 implementation, Branch C is the most common case for the first intersection of two non-overlapping shapes; Branch A is needed for nested or touching cases.

### 12.6 DoHorizontal (`engine.cpp:2526+`)

Horizontal edges are not part of the AEL; they are processed via a dedicated pass after Bot events at a given Y. The pass walks the AEL from the horizontal's left X to its right X, handling each AEL edge it crosses:

- If the AEL edge is at the horizontal's leading endpoint AND is a local maximum of the same OutRec: **AddLocalMaxPoly(horz, e, horz.top)** — close the ring through the horizontal's endpoint.
- Else if both horizontal and AEL edge are contributing at this point: **IntersectEdges(horz, e, pt)** — treat the horizontal-meets-AEL-edge as an intersection and dispatch through §12.5's table.
- Else: just advance.

Crucial phasing detail Clipper2 handles: a horizontal segment is *itself* a bound, with its own `local_min`. `PushHorz` queues it during Bot processing for execution after the rest of the Bot events at this Y are done (lines around 2131-2141). The two-phase ordering — Bots first, then horizontals — is the reverse of §11.8's prose. **Revise §11.8** to match: `Top < Bot < Horiz < Intersection` at the same scanline Y.

For a horizontal local minimum (the bottom of a polygon is a horizontal edge), the two vertical bounds emerging from its endpoints are Bot events at the same Y. They enter the AEL first; then the horizontal pass runs, sees both verticals as `AEL` entries, and calls `AddLocalMinPoly` on them with the horizontal's leftmost point. The horizontal itself contributes the bottom segment of the ring.

### 12.6.1 Execution plan: making horizontals first-class AEL edges (the "option 3" rework)

Status (2026-05-20): **complete.** Implemented on branch `refactor-dohorizontal`. All six stages below landed; the full `clip` + `polyclip` suite is green and the four previously-skipped tests (`TestUnionOverlappingSquaresVertexInsideOther` → area 184, and `TestIntersect/Difference/XorOverlappingAxisAligned`) pass. Horizontals are first-class AEL edges; the §11.7 synth-intersect machinery is deleted. The multi-edge-confluence follow-up (below) also landed.

**Follow-up (DONE 2026-05-20, `TestUnionCoincidentHorizConfluence` un-skipped, area 130):** a multi-edge confluence — one polygon's local-maximum apex coinciding with the other's boundary vertex, combined with a coincident bottom horizontal — was mis-handled: `closeBound` did not process the edges *between* a maxima pair the way Clipper2's `DoMaxima` (engine.cpp:2729) does, so hot/contributing status was never transferred across the confluence and the larger ring was dropped (returned area 65 instead of 130). Fixed in `clip/sweep.go`: `maximaPartner` now scans the whole AEL outward (not just immediate neighbours) so an interleaved pair (`a-L,b-L,a-R,b-R`, where a's max ≠ b's max) is matched, guarded by `scanMaximaPartner`'s apex-column test (an intermediate edge is crossed only if it lies on `maxPt.X` at the scanline — the "squeeze" that distinguishes a genuine confluence from two maxima that merely share a coordinate, e.g. overlapping diamonds). `resolveBetweenMaxima` then dispatches each between-edge through `IntersectEdges` at `maxPt` (Clipper2 DoMaxima's between-loop, engine.cpp:2756) before the pair closes via `AddLocalMaxPoly`. The pre-rework engine handled this case via the (now-deleted) synth-intersect mechanism.

**Missed-crossing-after-horizontals (DONE, `TestUnionSlantCoincidentBottom`):** `FuzzUnion` found a slanted-quad-meets-square case (`a=(-5,-5),(16,-5),(5,5),(-5,5)` ∪ `b=(15,-5),(25,-5),(25,5),(15,5)`, bottoms coincident over x∈[15,16]) where the union returned a degenerate triangle (area 150) because `a`'s slant–`b`'s-left-edge crossing was never scheduled: while the bottom horizontals were walked, `b`'s coincident horizontal sat transiently between the two bounds at the moment their neighbours were checked, then advanced away leaving them adjacent with no fresh intersection check. Root cause is the *incremental* intersection scheduling (schedule-on-adjacency-change) going stale across horizontal-pass AEL rearrangement. Fix in `clip/sweep.go`: after `flushPendingHoriz` settles the AEL, `rescanAdjacentIntersections` re-checks every adjacent pair and schedules missed crossings, guarded by a `pendingCross` counter so a crossing the incremental path already queued is not double-enqueued (a duplicate would swap the pair back). Gated on a horizontal pass having run. A differential test over thousands of random *simple* quad pairs showed zero out-of-bounds regressions and many corrections (the identity |A∪B|+|A∩B|=|A|+|B| now holds on inputs where it previously failed). Expected area for the case: 255 − 5/11.

**Still open (pre-existing, separate bugs):** `FuzzUnion`/`Intersect`/`Difference` still surface area-bound violations on other inputs (and `Xor` on the area identity), several even for simple polygons — the incremental scheduler and classification have further latent gaps. A more thorough fix is to recompute the per-scanbeam intersection list from the settled AEL (Clipper2's `DoIntersections`) instead of incremental scheduling; deferred.

**Why this is the correct fix.** The current engine excludes horizontals from the AEL and patches the resulting missed crossings with the §11.7 *synth-intersect* mechanism. That mechanism is Union-only (hence the skipped `TestIntersect/Difference/XorOverlappingAxisAligned`) and still misses interior crossings of a through-edge with a horizontal's span (`TestUnionOverlappingSquaresVertexInsideOther`, skipped). Clipper2 has no synth-intersect: every edge — horizontal or not — lives in the AEL and crossings flow through one `IntersectEdges` path. Option 3 = adopt that uniform model and delete the workaround.

**The crux — winding model.** `clip/classify.go:signedContribution` returns **0** for horizontals. That is only safe because horizontals are never in the AEL today. Once a horizontal is an AEL member, `Classify`'s left-walk would treat it as a same/other-source predecessor with `WindSelf` derived from a 0 contribution, corrupting neighbour winding. Clipper2 instead gives every edge a `wind_dx` of ±1 (the sign of the edge's bottom-vertex traversal direction) and computes `wind_cnt` from it (`SetWindCountForClosedPathEdge`, engine.cpp:1011). **Resolution:** a horizontal in the AEL must carry the `wind_dx`/contribution of the *bound it belongs to* (i.e. the sign of the bound's adjacent non-horizontal edge), not 0. The horizontal does not change the winding of a vertical ray that it lies along; what matters is that it carries its bound's contribution forward so neighbours classify correctly while it sits in the AEL.

**Stages (all on one branch; suite is red until the last):**

1. **Winding reconciliation.** Give each AEL edge a `WindDx` (±1) set from the bound's traversal direction; horizontals inherit their bound's `WindDx` rather than contributing 0. Rework `signedContribution`/`Classify` to use it. Re-green the existing non-horizontal tests first to confirm the new winding model is behaviour-preserving for the cases already passing.
2. **AEL membership.** `spawnBoundActive` stops skipping leading horizontals: the bound's `ActiveEdge` sits on its first segment even when horizontal, inserted at the near-X with a sweeping `CurrX`. `advanceBoundCursor` likewise stops the skip-and-emit-endpoints shortcut.
3. **`DoHorizontal` walk** (port of engine.cpp:2526). When a bound's cursor is on a horizontal, walk the AEL in the horizontal's direction: for each crossed edge call `IntersectEdges(horz, e, pt)` then swap positions, advancing `horz.CurrX`; at the bound's local-max vertex call `AddLocalMaxPoly`; otherwise promote the cursor (`UpdateEdgeIntoAEL` equivalent) to the next bound segment.
4. **Delete synth-intersect.** Remove `processSynthIntersectsAtLocalMin`, `synthIntersect`, `findSynthMaxPartner`, `boundLeadingHorizFarXs`, `emitLeadingHorizOutPts`, `boundHasInteriorVertex` and the legacy `handleHorizMin`/`handleHorizMax` per-edge path once `DoHorizontal` subsumes them.
5. **Event ordering.** Confirm/adjust the `EventKind` order to `Top < Bot(LocalMin) < Horiz < Intersection` (§12.6 phasing note).
6. **Re-green + un-skip.** Drive the full suite green; un-skip `TestUnionOverlappingSquaresVertexInsideOther` (expect area 184) and the three `Intersect/Difference/Xor` axial-overlap tests (the synth-intersect Union-only limitation is gone).

### 12.7 Pre-pass: identifying local minima

Before the sweep, walk each input ring once:

1. Find every vertex `v` where the Y-direction of incoming and outgoing edges *reverses* (descending then ascending → local minimum; ascending then descending → local maximum). Horizontal edges count toward the direction of their non-horizontal neighbours; a horizontal sandwiched between two ascending edges is part of an ascending bound, etc.
2. For each local minimum, emit a `LocalMinima` record with pointers to the two emerging bounds.

`LocMinSorter` (`engine.cpp:49`) then sorts local minima by Y ascending (X ascending for ties), which is the event queue's processing order.

### 12.8 Implementation order for future sessions

Suggested re-sequencing of the §11.11 increments now that we have Clipper2 as reference:

- **Increment 4'**: replace the current per-edge sweep with a bound-based one. Walk each input ring, identify local minima/maxima, build `LocalMinima` records, and reshape the event queue to fire on minima (not on every Segment's Bot/Top). Keep the existing `Segment` and `clip/intersect.go` machinery — they still apply to the individual edges within each bound. *Estimated: ~300 LoC change concentrated in clip/sweep.go and a new clip/bounds.go.*
- **Increment 5'**: `OutPt` / `OutRec` with `front_edge` / `back_edge` and the AddLocalMinPoly / AddLocalMaxPoly / JoinOutrecPaths trio. Use §12.3–§12.4 as the spec.
- **Increment 6'**: extend `IntersectEdges` (or `handleIntersection` in our code) with the §12.5 decision tree. Update §11.4's classification machinery to feed `IsHotEdge`.
- **Increment 7'**: `DoHorizontal` per §12.6, including the revised `Top < Bot < Horiz < Intersection` phasing.
- **Increment 8'**: postprocess (§11.9) and public `Union` in `boolean.go`.
- **Increment 9'**: adversarial suite (§6.2). Each adversarial case becomes its own test in `clip/` or top-level integration tests.

Each of these is one or more session's work; do not attempt to fold them.

### 12.9 What Clipper2 does that we deliberately omit

For first cut:

- `using_polytree_` and the hierarchical owner relationships among nested rings. Our hole-assignment (§11.9) is bbox-prefilter + point-in-polygon, which is simpler and sufficient for slicer use.
- Open paths (`is_open`). Polyclip's public API is closed-region only (DESIGN.md §3.6).
- `FixSelfIntersects` / `DoSplitOp`. Our preprocess (§11.2 step 5) splits overlaps up front; the sweep should never produce self-intersecting output rings. If it does we treat that as a bug and fix it in the sweep, not by a post-pass.
- `FillRule` other than NonZero. Polyclip standardises on the non-zero winding rule.

These omissions reduce the surface to translate by roughly half.

### 12.10 ActiveEdge lifecycle protocol for the bound model

When increment 4' (bound model) is wired through `handleTop`, several state machines have to coexist without trampling each other: bound-cursor advance, intersection swaps, OutRec front/back rewiring, and AEL ordering. This section captures the rules — pin them down before implementing, because a partial implementation hits subtle ordering bugs that are hard to diagnose after the fact. Reference points are `clipper.engine.cpp:1731` (`UpdateEdgeIntoAEL`) and `clipper.engine.cpp:2118-2145` (the main loop).

#### 12.10.1 Clipper2's scanbeam loop

```
loop:
  InsertLocalMinimaIntoAEL(y)         // spawn bounds at this scanline
  while PopHorz(e): DoHorizontal(e)   // process horizontals queued at bot
  bot_y = y
  y = PopScanline()                   // y = next scanline (top of beam)
  DoIntersections(y)                  // ALL intersections at bot_y < ip.y < y
  DoTopOfScanbeam(y)                  // Tops at y: maxima close, intermediate advance
  while PopHorz(e): DoHorizontal(e)   // horizontals queued by DoTopOfScanbeam
```

Three crucial properties:

1. **Intersections are processed strictly inside the scanbeam** — `DoIntersections` operates on a Y interval `(bot_y, top_y)`. Even if the algebra puts an intersection point at the exact top, Clipper2 clamps it inside the beam (`engine.cpp:2353-2374`). No intersection event ever fires at the same Y as a Top.

2. **Within `DoTopOfScanbeam`, the cursor advance is in place** — `UpdateEdgeIntoAEL` mutates the AE's `bot`, `top`, `vertex_top`, `curr_x`, `dx` fields but does NOT touch the AEL linked list. The AE stays where it is.

3. **Horizontals queued at advance fire AFTER all Top events at this scanline** — they're processed in a second `PopHorz` pass at the bottom of the loop, with the AEL already updated by all per-edge advances.

The protocol disciplines us against three pitfalls: scheduling intersections at Top vertices (don't — they're impossible by construction), reordering the AEL during cursor advance (don't — leave it for the next intersection pass to fix), and interleaving horizontal handling with Top advance for sibling edges (don't — finish all Top advances first).

#### 12.10.2 Mapping to polyclip's event queue

Polyclip's `EventQueue` is a single priority queue ordered by `(Y, X, Kind)`. To preserve the Clipper2 ordering:

- **EventIntersection.Y**: must satisfy `bot_y < ip.Y < top_y` for the scanbeam it belongs to. Already true by construction (`Intersect` only returns `ProperCross` when the open segment interiors meet; endpoint-touches are `Touch` and not scheduled).
- **EventTop.Y**: at the Top vertex's Y. Fires after every EventIntersection at smaller Y, before any event at greater Y.
- **EventLocalMin.Y**: at the local-min vertex Y. Same Y as the corresponding bound's first EventTop only when the bound is single-segment with `Bot.Y == Top.Y` — impossible for non-horizontal bounds.
- **EventHoriz / EventHorizMaxOpen**: legacy per-edge path. With the bound model fully wired, these are unreachable for closed-ring inputs.

EventKind ties at the same `(Y, X)` go through `EventHorizMaxOpen < EventTop < EventBot < EventLocalMin < EventHoriz < EventIntersection`. EventLocalMin sits after EventBot so a bound spawned at this scanline can see existing AEL state (its claimed Bots were suppressed in `newSweep`); EventIntersection is last and only fires when the algebra coincidentally puts the intersection X at the Top vertex's X — defensively scheduled, dispatched safely by `handleIntersection`'s adjacency check.

#### 12.10.3 ActiveEdge field mutations

`ActiveEdge` has eight fields whose mutation must follow a strict protocol:

| Field | When mutated | By |
|---|---|---|
| `Seg` | At cursor advance | `advanceBoundCursor` (in place) |
| `Bound` | Set once at spawn, never reassigned | `handleLocalMin` only |
| `EdgeIdx` | At cursor advance | `advanceBoundCursor` (in place) |
| `CurrX` | At cursor advance + every scanline update | `advanceBoundCursor`, `AEL.UpdateForScanline` |
| `WindSelf` | At spawn + intersection swap | `Classify`, `IntersectEdges` |
| `WindOther` | At spawn + intersection swap | `Classify`, `IntersectEdges` |
| `Contributing` | At spawn + intersection re-classification | `Classify` |
| `Outrec` | At AddLocalMinPoly + SwapOutrecs + AddLocalMaxPoly close | `AddLocalMinPoly`, `SwapOutrecs`, `AddLocalMaxPoly`, `JoinOutrecPaths` |

Key invariant: **`Bound` is geometric; `Outrec` is logical**. An AE's `Bound` always refers to the input ring's bound that this AE is sweeping — it never changes after `handleLocalMin` spawns it. The AE's `Outrec` (and whether it's `FrontEdge` or `BackEdge` of that OutRec) DOES change at intersections via `SwapOutrecs`/`JoinOutrecPaths`. Cursor advance only consults `Bound`/`EdgeIdx`; it never inspects or mutates `Outrec`.

#### 12.10.4 Cursor advance procedure (`advanceBoundCursor`)

```
advanceBoundCursor(ae, currentTop):
  // Emit at the current edge's Top before advancing.
  if ae.Contributing && ae.IsHotEdge():
    AddOutPt(ae, currentTop)

  // Walk forward through any horizontals between current and next non-horizontal.
  next = ae.EdgeIdx + 1
  horizontals = []
  while next < len(ae.Bound.Segs) && ae.Bound.Segs[next].Horizontal():
    horizontals.append(ae.Bound.Segs[next])
    next += 1

  if next >= len(ae.Bound.Segs):
    // Trailing horizontals at local max → delegate to closeBound.
    closeBound(ae, horizontals, currentTop.Y)
    return

  // Mid-bound horizontals: emit OutPt at each far endpoint.
  for h in horizontals:
    if ae.Contributing && ae.IsHotEdge():
      AddOutPt(ae, {X: boundFarX(ae.Bound, h), Y: currentTop.Y})

  // IN-PLACE update — DO NOT remove/reinsert in the AEL.
  ae.EdgeIdx = next
  ae.Seg = ae.Bound.Segs[next]
  ae.CurrX = ae.Seg.Bot.X

  // Schedule next EventTop for the new current edge.
  queue.Push(EventTop{P: ae.Seg.Top, SegA: ae.Seg})
```

**Critical:** no `s.ael.Remove(ae)` and no `s.ael.Insert(ae)`. The AE keeps its AEL position. The new edge's slope may differ from the old, but the AEL ordering is fixed up by the NEXT scanbeam's `DoIntersections` pass, which will fire intersection events for any crossings caused by the slope change. This mirrors Clipper2's `UpdateEdgeIntoAEL` (`engine.cpp:1731`) exactly.

A previous attempted implementation in this codebase did remove/reinsert during advance — it broke `TestUnionOverlappingDiamonds` (Union area 143.75 < expected 200) because the reinsertion disrupted adjacency invariants that pending intersection events relied on. The fix is the in-place protocol above.

#### 12.10.5 Close procedure (`closeBound`)

Called when a bound's cursor has either reached its last segment (no trailing horizontals) or walked through its trailing horizontals. The local-max vertex is:

- Last segment's `Top` if no trailing horizontals.
- Far endpoint of the last trailing horizontal otherwise (in bound traversal direction).

Pairing protocol (when the partner — the OutRec's other edge — is at its own bound's last):

```
closeBound(ae, trailingHorizontals, y):
  maxPt = (trailingHorizontals nonempty) ? boundFarX(...) : ae.Seg.Top
  partner = OutRec partner of ae (FrontEdge if ae==BackEdge, else BackEdge)

  if partner == nil || !partner.IsBoundLast():
    // Asymmetric case: partner not yet at its end. Leave the close to the
    // partner's eventual closeBound call. Emit maxPt on ae's chain and
    // remove ae from AEL — the partner still references the OutRec.
    if ae.Contributing && ae.IsHotEdge():
      AddOutPt(ae, maxPt)
    ael.Remove(ae)
    delete(bySeg, ae.Seg)
    return

  // Both at end. AddLocalMaxPoly closes; FrontEdge passed first by
  // convention so the local-max vertex prepends to Pts.
  front, back = (ae.IsFront() ? ae : partner), (ae.IsFront() ? partner : ae)
  if hot(front) && hot(back):
    AddLocalMaxPoly(ael, front, back, maxPt)
  ael.Remove(ae)
  ael.Remove(partner)
  delete(bySeg, ae.Seg)
  delete(bySeg, partner.Seg)
```

For the symmetric case (both bounds reach their end at the same Y, which holds for axial rectangles, diamonds, the W-shape, and well-formed staircases), `partner.IsBoundLast()` is always true at the moment the first bound calls `closeBound`. The asymmetric branch is defensive — it can occur with self-touching polygons or intersected-then-rejoined bounds, both of which Phase 2 first-cut does not cover (DESIGN §12.9).

#### 12.10.6 What `claim-all` does (and why it's required)

To activate the bound model fully, ALL segments of every bound from `BuildLocalMinima` must be "claimed" — their per-segment Bot/Top/Horiz events are NOT scheduled. `handleLocalMin` spawns the AE with `Bound` and `EdgeIdx=firstNonHorizontalIdx`. `handleTop` (called from the bound-model EventTop scheduled lazily by `handleLocalMin` / `advanceBoundCursor`) handles all subsequent transitions via the in-place advance protocol above.

The previous `claim-spawn` heuristic (claim only leading horizontals + first non-horizontal) leaves mid-bound horizontals and intermediate non-horizontals on the per-segment path, where `handleHoriz` / `handleThroughVertex` handle them. This works for axial rectangles and diamonds — but breaks for any polygon with mid-bound horizontals (staircases) because `ClassifyHorizontals` returns `HorizClassMid` for them, with no per-segment handler.

`claim-all` requires the in-place cursor advance from §12.10.4. Switching to `claim-all` without fixing the advance was the chain that broke `TestUnionOverlappingDiamonds`.

#### 12.10.7 Implementation checklist for the next attempt

In order, each as its own commit:

1. **Restore `claim-all`** in `newSweep`. Claim every segment of every bound. Skip per-segment events for all claimed segments.
2. **Re-introduce `advanceBoundCursor` with in-place update** per §12.10.4. No `Remove`/`Insert` calls on `ael`.
3. **Re-introduce `closeBound`** per §12.10.5.
4. **Lazy-schedule the first `EventTop`** in `handleLocalMin` after spawning each bound's AE.
5. **Make `handleTop` bound-aware**: if `ae.Bound != nil` and `!ae.IsBoundLast()`, call `advanceBoundCursor`; if `ae.Bound != nil` and `ae.IsBoundLast()`, call `closeBound(ae, nil, e.P.Y)`; else legacy path.
6. **Verify ring tests still pass** — diamonds (single + disjoint), axial rectangles (single + disjoint + nested), W-shape, overlapping diamonds, touching boundary (still error per byStart ambiguity).
7. **Add a staircase test** that confirms the L-shape from §12.10.5's worked trace (6 vertices, CCW, mid-bound horizontal) produces a correct closed ring with positive signed area.

Each step in isolation should keep tests green. The integration is fragile because of the cross-cutting state machines; isolating per commit makes regressions easy to bisect.

#### 12.10.8 Lessons from the integration that the protocol must capture

Three issues surfaced during implementation that aren't obvious from the algorithm alone. These rules are load-bearing — violating any one of them breaks overlapping-shape Union for non-obvious reasons.

**Rule 1: `handleLocalMaximum` gates on `IsHotEdge`, not `Contributing`.**

At a local maximum where two AEs from DIFFERENT OutRecs meet (overlapping shapes), the rings must be joined via `AddLocalMaxPoly` + `JoinOutrecPaths`. After an upstream `IntersectEdges` swap and reclassification, one of the AEs may have `Contributing=false` (its post-swap winding makes it interior to the union) yet still be in a hot OutRec that needs joining. Gating on `Contributing` skips the join, leaving the ring open with the top half disconnected from the cycle.

```go
// WRONG:
if ae1.Contributing && ae2.Contributing { AddLocalMaxPoly(...) }
// RIGHT:
if ae1.IsHotEdge() && ae2.IsHotEdge() { AddLocalMaxPoly(...) }
```

Diagnostic signature: `Union` of two overlapping shapes produces a ring with only the bottom-half vertices; the top half is missing. The `r.Rings` list shows two rings — one with the bottom vertices and one with `Pts=nil` (orphaned `NewOutrec` from the upper intersection's `AddLocalMinPoly`).

**Rule 2: `advanceBoundCursor` MUST call `maybeScheduleIntersect` against the new segment's neighbours.**

After in-place cursor advance, the AE's segment changes. Its OLD segment's crossings have been resolved (or aged out), but the NEW segment may cross neighbours that the old one didn't — its slope is different. Without scheduling fresh intersection checks, future scanbeam intersections silently never fire.

```go
// In advanceBoundCursor, after `ae.Seg = b.Segs[next]; ae.CurrX = ...`:
i := s.ael.IndexOf(ae)
if i >= 0 {
    if left := s.ael.LeftOf(i); left != nil {
        s.maybeScheduleIntersect(left, ae, currentTop.Y)
    }
    if right := s.ael.RightOf(i); right != nil {
        s.maybeScheduleIntersect(ae, right, currentTop.Y)
    }
}
```

Diagnostic signature: an `EventIntersection` you expect (e.g. `(2.5, 7.5)` for overlapping diamonds' top crossing) is absent from the trace. The intersection point is geometrically present in the inputs but the sweep never reaches it because no event was scheduled. This compounds with Rule 1 — the missed intersection leaves two AEs cold that should have become hot at the second intersection's `AddLocalMinPoly`.

**Rule 3: `newSweep` tries `BuildLocalMinima` BEFORE `ClassifyHorizontals`.**

`ClassifyHorizontals` strictly rejects mid-bound horizontals (`HorizClassMid`) — staircases would fail with `ErrUnsupportedHorizontal` before the bound model ever ran. But the bound model HANDLES mid-bound horizontals natively (they're regular `Bound.Segs` entries that `advanceBoundCursor` walks through). Reordering:

```go
// In newSweep:
mins, mErr := BuildLocalMinima(s.segs)
if mErr == nil {
    // Bound model active. Schedule EventLocalMins, claim all bound segments.
    // ClassifyHorizontals is NOT called — bound model owns horizontal handling.
} else {
    // Fall back to per-edge dispatch with strict ClassifyHorizontals.
    hinfo, hErr := ClassifyHorizontals(s.segs)
    if hErr != nil { s.err = hErr; return s }
    s.horiz = hinfo
}
```

Diagnostic signature: any closed-ring input with a mid-bound horizontal (e.g. an L-shape staircase polygon `(0,0)→(2,0)→(2,2)→(4,2)→(4,4)→(0,4)`) errors with `ErrUnsupportedHorizontal` even though the bound model could handle it.


### 12.11 The general-crossing correctness gap and the DoIntersections rework

**Status (2026-05-20): implemented for general position; degenerate positions WIP on branch `feat-dointersections` (not merged).** The per-scanbeam recompute is in `clip/sweep.go` (`doIntersections`/`buildIntersectList`/`processIntersectList`); incremental scheduling is gated off under the bound model (kept only for the legacy fallback). Measured vs the Monte-Carlo oracle, random simple quad pairs *with* boundary crossings, coords in [-1000,1000]:

| input class | before | after |
|-------------|--------|-------|
| convex-convex (with crossings) | ~49% wrong | **0.0%** |
| non-convex involved | ~57% wrong | **8.3%** |

So general-position crossings are fixed outright for convex inputs and dramatically improved for non-convex. `buildIntersectList` uses the half-open beam `(botY, topY]` (Clipper2 clamps boundary crossings inward rather than dropping them).

**Degenerate-position handling — partial.** Where a vertex of one polygon lies exactly on an edge of the other, or crossings land on a vertex scanline, the per-scanbeam recompute is not enough; the coincidence must be processed at the corresponding vertex event. Done so far:

- **Local-min vertex-on-edge** (`handleLocalMin`): the right bound is now inserted ADJACENT to the left bound and bubbled into sorted order, calling `IntersectEdges` at the local-min point for every edge it passes (Clipper2's `InsertLocalMinimaIntoAEL` + `IsValidAelOrder` bubble). This fixes the minimal repro `a=(1,1),(-1,2),(0,-2),(2,0)` ∪ `b=(-2,1),(-5,0),(0,-3),(0,1)` (a's vertex (0,-2) on b's x=0 edge): Union now 15.375 (was 11.0).

- **T-junction normalization — `SplitTJunctions` (2026-05-20).** A preprocessing pass (run after `SplitOverlaps`, before `DedupCoincidentEdges`) splits any segment whose open interior is touched by a *vertex* of another segment, inserting that vertex as a shared endpoint. The split point is the touching vertex — an existing grid coordinate — so no new rounding is introduced and the transform is area-preserving. This establishes the invariant "no vertex lies in the open interior of any edge", the sibling of `SplitOverlaps`'s "no partial collinear overlaps". It is a **necessary precondition** for the shared-vertex crossing dispatch below, but **does not on its own** change any boolean result: it converts a vertex-on-edge into a coincident *shared vertex*, which the sweep then mishandles in the same way (see Residual).

Done / still open:

- **Incremental wind-count maintenance — DONE (2026-05-20).** `Classify` and `IntersectEdges` now follow Clipper2's incremental `wind_cnt`/`wind_cnt2` model instead of the old pre-swap-snapshot + `Classify` left-walk recompute:
  - `Classify` transcribes `SetWindCountForClosedPathEdge` (engine.cpp:1011, NonZero): `WindSelf` is the *higher* winding of the two regions touching the edge (the reversing-direction and now-outside cases the old `prevSelf+delta` dropped), and `WindOther` is the running sum of the other source's `WindDx` to the left.
  - `IntersectEdges` updates the two edges' counts *in place* at the crossing (same polytype: the `wind_cnt += wind_dx` with the `==0 ⇒ negate` rule; cross polytype: `wind_cnt2 += wind_dx`), keys the §12.5 dispatch on the **post-update** `abs(WindSelf)`, adds Clipper2's `abs(wind_cnt) ∈ {0,1}` guard (engine.cpp:1932), and no longer re-runs the left-walk after the swap. `isContributing` now matches `IsContributingClosed` (engine.cpp:908): `abs(WindSelf)==1` plus the per-op `WindOther` test, dropping the old `flips` heuristic.
  - Effect: the front/back polarity no longer drifts for the simple/general-position cases; the random simple-quad differential is unchanged (it never exercised the divergent nesting/deep-winding cases) and convex crossings stay at 0%. Cumulative Union of the overlapping diamonds improved 475 → 489 (truth 550). The clip unit tests that asserted the old `WindSelf` "winding to the right" semantics were recalibrated to the correct Clipper2 `wind_cnt` values (Contributing outcomes were unchanged).
- **General non-convex crossing-splice — FIXED (2026-05-20); `TestUnionAllManyOverlapping` un-skipped and passing.** Two coupled premature-mutation bugs broke `getPrevHotEdge`'s search for the *enclosing* hot edge, which is what sets a crossing-spawned ring's front/back orientation:
  1. **Premature AEL swap in `IntersectEdges`.** polyclip swapped the two edges' AEL positions *before* running the §12.5 dispatch; Clipper2 runs `IntersectEdges` with the AEL still in pre-crossing order and swaps only afterwards (`engine.cpp:2461-2462` — `SwapPositionsInAEL` is the caller's job). The winding update is by edge identity so the early swap looked harmless, but `AddLocalMinPoly`'s `getPrevHotEdge` walks the AEL *by position*: with the swap already applied, at the upper crossing it returned the just-made-hot partner `e2` itself instead of the genuine enclosing edge. Fix: defer `ael.SwapAt(i1)` to the end of `IntersectEdges` (unconditionally, including the guard-return path).
  2. **`AddLocalMinPoly` argument-order dependence.** `handleLocalMin` calls `AddLocalMinPoly(rightAE, leftAE, …)` (e1 = right bound, to keep FrontEdge = right per polyclip's CCW mirror of Clipper2), and the body called `getPrevHotEdge(e1)` — i.e. walked left from the *right* bound and hit the *left* partner first (whose sides aren't set yet, so `outrecIsAscending` is spuriously false). This masked the real enclosing-edge nesting parity for every input local minimum; convex/simple cases survived only because the partner's spurious `ascending=false` happened to match the no-enclosing-edge answer. Fix: resolve `left`/`right` from current AEL positions and walk `getPrevHotEdge(left)`, so the partner (always to the right of `left`) is never returned and the orientation is argument-order-independent and Clipper2-equivalent.

  With both fixes the minimal reproducer below yields the exact correct ring (area 33.30, CCW) and the d50048a `SwapFrontBackSides` workaround no longer fires for the diamond/repro cases. The whole existing suite (incl. the CCW ring-orientation tests) still passes; three `clip` unit tests that asserted the old argument-order-dependent side assignment were recalibrated. The random simple-quad differential barely moves (non-convex "U or I wrong" 5.6%→5.0%) because it under-samples the "one edge crosses the other polygon twice" non-convex config that this bug needs; the headline regression test (`TestUnionAllManyOverlapping`, 5 overlapping diamonds) is the sensitive check.

  **Residual — re-scoped to shared-vertex crossing dispatch (2026-05-20).** The d50048a workaround in `AddLocalMaxPoly` is *not* yet dead, but the earlier hypothesis (a general-position bug "in another event path — through-vertex/maxima or `SwapOutrecs` at deeper nesting") is **disproven**. A throwaway differential (a probe counting the workaround branch over 400k random non-convex simple-quad pairs, coords in [0,30], vs the MC oracle) gives a decisive split:

  - **In true general position the workaround never fires** (0 firings; the previous session's deferred-AEL-swap + AEL-position-resolved `AddLocalMinPoly` fully fixed the general-position crossing-splice orientation bug).
  - **Every one of the 23 firings is a vertex-on-edge degeneracy** — a vertex of one polygon lying *exactly* on an edge of the other (`minVE = 0.000`, even for the firings that are otherwise general-position with distinct X/Y). And in all of them the workaround does **not** recover — the area is wrong regardless. The whole existing suite, incl. `TestUnionAllManyOverlapping`, passes with the workaround replaced by `return nil`.

  So the residual is the **vertex-on-edge / shared-vertex track**, not the general-crossing track. `SplitTJunctions` (above) reduces a vertex-on-edge to a clean *shared vertex*, but the root cause is then exposed: **two bounds that swap AEL order exactly at a coincident vertex are never dispatched as a crossing.** `doIntersections` finds only a `ProperCross` strictly inside the open beam `(botY, topY)`; an at-vertex crossing is a `Touch` on the beam boundary, so `IntersectEdges` is never called there, the hot/cold status fails to flip, and the maxima invariant (both edges of a closed-path local maximum share hotness — Clipper2 `DoMaxima`, engine.cpp:2777, checks only `IsHotEdge(e)`) breaks, surfacing as the same-side `AddLocalMaxPoly` the workaround patches.

  **Worked example** (`A=(1,8),(16,11),(21,5),(29,24)` ∪ `B=(6,9),(24,7),(19,28),(13,29)`; engine 75.5 vs truth 298.5): B's vertex (6,9) lies on A's edge (1,8)→(16,11). `SplitTJunctions` inserts (6,9) into A's bound; at that shared vertex A is left of B below, right of B above — a real crossing — but it is never dispatched, so A's front bound stays hot through B's interior.

  **Shared-vertex crossing dispatch — DONE (2026-05-20, this session).** Added `sweep.reconcileSharedVertexCrossings(y)`, called after the Tops phase in `handleScanlineBound`. After every cursor has advanced through a scanline's vertices, any two AEL-adjacent edges with equal `CurrX` that are now out of slope order have crossed at that shared vertex (post-`SplitTJunctions`, an edge with the point strictly interior would have been split, so `CurrX == V.X` ⟺ the point is a vertex of that edge's bound). `IntersectEdges` is dispatched for each such inversion and bubbled until no adjacent inversion remains — the through-vertex analog of `handleLocalMin`'s `IsValidAelOrder` bubble. The worked example below now yields 298.0 (truth ~298.5).

  Measured over the simple-quad differential (random *simple* CCW integer quads, coords [0,30], 20k pairs/op, vs the MC oracle), before → after:

  | op | wrong rate | workaround firings |
  |----|-----------|--------------------|
  | Union | 0.53% → 0.15% | 2 → **0** |
  | Intersect | 0.20% → 0.04% | 1 → **0** |
  | Difference | 0.24% → 0.04% | 0 → **0** |
  | Xor | 0.47% → 0.04% | 110 → **11** |

  General position (coords [-1000,1000]) stays at 0% wrong / 0 firings — no regression. The whole existing suite passes; `TestUnionSharedVertexCrossing` added as the regression guard.

  **Residual — now Xor-only, deferred to the Xor classification track.** Firings reached 0 for Union/Intersect/Difference; only Xor still reaches the `AddLocalMaxPoly` same-side branch (~11×). With the shared-vertex fix in place, replacing the workaround with `return nil` (Clipper2's hard-error) leaves the whole suite green and the differential wrong-rates unchanged (Xor 7→8, MC noise) — i.e. the recovery is doing no real work and the residual firings are the **separately-tracked Xor classification gap** below, not a shared-vertex problem. The workaround is kept only as a safety net until that track lands, at which point it should be deleted. (Decision 2026-05-20: keep + defer, do not couple workaround deletion to the unresolved Xor track.)

  **Classification audit — 2026-05-20 (later session): the "Xor/Difference classification gap" is STALE; the residual is degenerate-position.** A fresh throwaway differential (random simple CCW integer quads vs the MC area oracle, *bucketed by clean vs degenerate* and with a workaround-firing probe in `AddLocalMaxPoly`) overturns the classification hypothesis below:
  - **Clean (non-degenerate) inputs: 0.00% wrong for all four ops** (Union/Intersect/Difference/**Xor**), coords [0,30]. The general-position rework + wind-count rewrite + shared-vertex dispatch already fixed general-position classification outright. There is no Xor/Difference classification gap to chase.
  - The scary large-coord numbers (e.g. "Intersect 12% @ [0,2500]") were **MC oracle noise**, not engine error: a fixed sample count gives absolute noise ∝ bbox area, which swamps small Intersect slivers. Scaling sample count with bbox area collapsed it 12.3%→2.5%→(trending to 0) as N grew — the signature of noise, since an engine bug would not shrink with more oracle samples.
  - **All remaining failures are degenerate-position** (shared vertex / vertex-on-edge): 0.66–3.63% on degenerate pairs across ops. Every workaround firing is Xor-only, degenerate-only, and **never recovers** (wrong-with-firing = 100% of firings) — confirming the recovery does no real work, consistent with the "keep as safety net" note above.
  - `isContributing` and `branchNeitherHot` were verified line-by-line against Clipper2 (`IsContributingClosed` engine.cpp:908, `IntersectEdges` engine.cpp:2008-2079) and are correct for NonZero. The one divergence found — `branchNeitherHot` compares **signed** `WindOther` where Clipper2-NonZero uses **abs** — is a **behavioral no-op** under polyclip's input contract: `validate.go` requires CCW outers / CW holes, so a valid MultiPolygon's other-source winding to the left is always 0 or +1 (never negative), making signed and abs identical. (`WindOther` can only go negative for malformed CW-outer input, which the contract excludes, and which `runBooleanOp` does not currently normalize/reject — see the orientation-invariant note below.) Deliberately NOT changed, to avoid robustness code for an input that cannot occur.
  - **Minimal degenerate repros** (smallest by bbox; simple CCW integer quads, shared vertex): `Union A=(7,9),(9,5),(8,10),(8,13) ∪ B=(7,3),(9,5),(8,13),(8,12)` got 4.364 vs truth 10.309. And one pair fails Intersect/Difference/Xor together — `A=(3,0),(6,0),(8,1),(5,2)`, `B=(6,0),(11,1),(5,1),(0,1)` (shared vertex (6,0)): Intersect→0 (truth 2.92), Difference→5.5 = full |A| (truth 2.63), Xor→11 = |A|+|B| (truth 5.32). The engine treats the pair as **non-interacting** — the shared-vertex crossing is never dispatched, so neither source's winding registers the overlap. (Note: that B has a collinear vertex (5,1) on edge (11,1)-(0,1), i.e. it is also self-degenerate; C should refine repro capture to reject within-polygon collinearity and prefer single-shared-vertex pairs.)

  **Track C (degenerate-position crossings) — increment 1 landed: post-horizontal-flush reconcile.** Root cause for one major class: when a bound reaches a shared vertex via its bottom **horizontal** (the horizontal's far endpoint coincides with another source's local-min / through vertex), the crossing against the edges sitting at that vertex was never dispatched. `doHorizontal` deliberately does not cross edges exactly at its far endpoint, and the far endpoint is only settled into the AEL *after* `flushPendingHoriz` — which runs after `handleScanlineBound`'s single (pre-local-min) `reconcileSharedVertexCrossings`. So the spawned local-min bounds and the promoted horizontal bound were never reconciled, threading the ring through the shared vertex twice (a self-touch) and over-counting. Fix: call `reconcileSharedVertexCrossings(y)` again in `run()` *after* `flushPendingHoriz`, once every cursor at `y` has settled. Minimal repro `A=(8,3),(9,5),(1,4),(4,4) ∪ B=(6,3),(8,3),(10,5),(5,4)` (shared (8,3)): Union 9.0→7.556 (truth ~7.52); regression guard `TestUnionSharedVertexViaHorizontal`. Measured net effect on single-shared-vertex pairs (random simple CCW quads [0,12], 3000 pairs, vs MC oracle), before→after: Union 12.6%→8.2%, Intersect 12.5%→7.9%, Difference 7.4%→2.8%, Xor 4.8%→0.2%. No regressions; whole suite green.

  **Track C — increment 2 LANDED: shared local-MAX confluence (2026-05-20).** The dominant residual single-shared-vertex class was two sources reaching their local **maximum** at the SAME vertex — four bounds (two per source) converging on one apex, with a cross-source crossing just below. Repro `A=(9,2),(10,4),(8,6),(8,4) ∪ B=(7,0),(8,3),(9,4),(8,6)` (shared max (8,6)): Union 1.333 (≈ the *intersection* area) vs truth ~5.67 — everything above the lower crossing dropped. **Root cause:** `isMaximaPartner` paired by bare *coordinate coincidence* (`boundMaxPt(cand) == maxPt`), so at the apex a HOT bound (e.g. B's left, on the union boundary, ring r0) grabbed the nearest coincident *other-source* edge (A's left, cold, interior) as its maxima partner instead of its own polygon's other bound. The hot ring spanning both sources (after the lower crossing merged A∪B into r0) was then never closed at the apex. **Fix:** require `cand.Seg.Src == ae.Seg.Src` in `isMaximaPartner` — Clipper2's `GetMaximaPair` pairs by `vertex_top` POINTER identity (the two bounds meeting at the same physical apex of the same input ring); same-source is the proxy. With it, B's left pairs with B's right (both cold→ the between-cross via `resolveBetweenMaxima` transfers r0 onto A's surviving bound), and A's left pairs with A's right, which then closes r0 at the apex. **Output dedup:** the confluence emits the apex twice (once in `IntersectEdges`'s one-hot branch when the hot maxima edge is crossed past the cold co-maximum edge, once in `AddLocalMaxPoly`), leaving a zero-length edge; `boolean.go`'s `dedupConsecutive` strips consecutive-identical points (incl. wrap-around) when building output rings, the analog of Clipper2's `BuildPath` cleanup. Regression guard `TestUnionSharedLocalMaxConfluence`. Measured (apples-to-apples, same 941 single-shared-vertex CCW-quad pairs [0,12] vs MC oracle), before→after: **Union 7.86%→0.85%, Intersect 6.48%→0.21%, Difference 3.19%→0.64%**, Xor 0.21%→0.43% (noise). General position [-1000,1000] stays **0.00%** (no regression); whole suite green.

  **Track C — remaining (next increment).** Residual single-shared-vertex Union ~0.85% / Difference ~0.64% remain. One known fragility: the four apex maxima are processed in *event-pop order*, not AEL (left-to-right) order as Clipper2's `DoTopOfScanbeam` does; when a cold co-maximum edge's `closeBound` fires before the leftmost hot one, `resolveBetweenMaxima` can cross two cold cross-source edges and spawn a spurious local-min ring. The repro happens to pop in a benign order; making the maxima sweep AEL-ordered (collect maxima tops, process leftmost-first) would harden this. Then: re-run the clean-vs-degen differential → drive the residual down and Xor `AddLocalMaxPoly` same-side firings → 0 → delete the d50048a workaround. Separately, the **orientation invariant** (`runBooleanOp` assumes CCW-outer/CW-hole but never enforces it via `validate.go`) is a small boundary-enforcement increment worth doing on its own.

  **Track C — AEL-ordering attempt: DEADEND; residual re-rooted to AddLocalMinPoly reflex-min parity (2026-05-20, later session).** Implemented the AEL-ordered maxima sweep above (sort the `EventTop` events in `handleScanlineBound` leftmost-first by current AEL index before processing, the port of Clipper2 `DoTopOfScanbeam`'s front-to-back walk). It is **not** the fix for the residual. Measured on a throwaway single-shared-vertex differential (random simple CCW integer quads sharing exactly one vertex, no within-quad collinearity, coords [0,12], 4000 pairs/op, vs the bbox-scaled MC oracle), event-pop → AEL-ordered: Union 0.82%→0.78%, Difference 0.57%→0.55%, Intersect 0.00%→0.00%, Xor 0.20%→0.20%. Diffing the wrong-sets: the change **fixes 4 pairs but regresses 1** (a previously-correct Xor case), all within the *same* degenerate class — i.e. it is shuffling deck chairs, not fixing a root cause.

  Tracing the regression `Xor(A=(4,1),(12,10),(10,9),(9,12) ; B=(3,4),(7,5),(10,9),(2,9))` — shared vertex (10,9), simultaneously A's reflex (concave-notch) local **min** and the right end of B's horizontal top (B's local **max**) — pinned the true cause. Event-pop and AEL-ordered traces are *identical* until A's local maximum at (12,10), where **both** orders reach the `IsFront(e1)==IsFront(e2)` same-side inconsistency in `AddLocalMaxPoly` (the d50048a workaround branch); they differ only in which of e1/e2 is passed first, and the workaround resolves the two argument orders to different areas (29.924 ≈ correct vs 12.379 wrong). So AEL-ordering only changes *which inputs* land in the unresolved workaround — a coin-flip, not a correction. Reverted.

  **Root cause (localized, not yet fixed).** The same-side collision at (12,10) originates at A's reflex notch min (10,9). `AddLocalMinPoly` computes the new ring's orientation as `frontIsRight = outrecIsAscending(prevHot) == isNew`. Here `isNew=true` (a genuine vertex min) and `getPrevHotEdge` correctly returns A's enclosing left bound (4,1)→(9,12); but that bound had been **reoriented to FRONT (`asc=true`) by an earlier crossing-respawn** — the `isNew=false` `AddLocalMinPoly` fired by the `(5.67,4.67)` crossing where B's edge cuts it. So the formula yields `true==true ⇒ frontIsRight=true` (Front = right edge (10,9)→(12,10)), whereas the concave min geometrically requires `frontIsRight=false` (Front = left edge (10,9)→(9,12)). With the wrong side, the lower ring's front (4,1)→(12,10) meets the notch ring's front at the (12,10) maximum → two fronts → the d50048a workaround. The parity formula is sound for a min enclosed by an edge whose front/back reflects a single input's nesting, but **breaks when the enclosing edge's orientation was set by a prior cross-source crossing-respawn rather than by its own input's winding** — the merged ring's front/back no longer encodes the nesting parity the formula assumes. This is the genuine d50048a root cause (an OutRec ownership/orientation-model gap, ≈ Clipper2's `SetOwner`/split machinery that polyclip's simplified model elides), NOT the maxima sweep order. **Next increment:** fix `AddLocalMinPoly` orientation for a vertex-min whose enclosing hot edge was reoriented by a crossing-respawn (port Clipper2's owner-based parity, or recompute the enclosing parity from winding rather than from the possibly-reoriented `FrontEdge==hotEdge` proxy), validate with the regression repro above + the clean-vs-degen differential, then delete d50048a. Method unchanged: throwaway differential (single-shared-vertex CCW quads vs bbox-scaled MC oracle) + a clip-package descale-aware trace hook (`dbg`/`DbgUnsnap` in a throwaway `clip/zz_dbg.go`, calls in `handleScanlineBound`/`closeBound`/`IntersectEdges`/`AddLocalMin`/`AddLocalMaxPoly`), rebuilt and deleted each session.

  **Winding-derived-parity attempt — DEADEND (2026-05-20, later session).** The lighter of the two proposed fixes above — "recompute the enclosing parity from winding rather than from the `FrontEdge==hotEdge` proxy" — was implemented and **empirically refuted**. The change replaced `outrecIsAscending(prevHot)` (= `prevHot.Outrec.FrontEdge == prevHot`) with a winding-derived test `prevHot.WindDx < 0` (i.e. "prevHot is input-ascending, Bot→Top"). Rationale at the time: for a *correctly oriented* ring under polyclip's mirror convention the front side is the input-ascending edge, so `outrecIsAscending(e)` *appears* to coincide with `e.WindDx < 0`, and `WindDx` is a per-bound constant immune to the cross-source respawn that scrambles `FrontEdge`. It fixes the regression repro exactly (at the (10,9) reflex min `frontIsRight` flips true→false, the (12,10) maximum's two edges become opposite-sided, the workaround firing drops 1→0, area stays 29.924≈truth). **But it badly regresses everything else.** A/B differential over 4000 single-shared-vertex CCW-quad pairs [0,12] (same harness run with old proxy vs new WindDx, shared MC truth), OLD→NEW: Union 1.15%→3.95% (fires 12→355), Difference 0.40%→2.98% (6→305), Xor 1.00%→9.00% (38→968), Intersect 0.05%→0.05% (0→20). So the premise is **false**: `outrecIsAscending` and `WindDx<0` do NOT coincide in general — the `FrontEdge` proxy *correctly* differs from local `WindDx` for legitimately hole-oriented / cross-source-merged rings (where the output ring winds opposite to the local input direction), and the proxy is right in the overwhelming majority of cases. The reflex-after-respawn case is a genuine *upstream* mis-orientation (the respawn ring's front/back is itself wrong, and the proxy faithfully reports that wrong value); bypassing the proxy with `WindDx` only "fixes" the one case by coincidence while discarding the correct hole/merge orientation everywhere else. Reverted. **Conclusion:** the residual cannot be fixed by a local parity tweak in `AddLocalMinPoly`; it requires the *heavier* option — maintaining a correct OutRec orientation/ownership model through `JoinOutrecPaths` and the crossing-respawn (port Clipper2's `SetOwner`/`Split`/`SwapFrontBackSides` machinery so `FrontEdge` stays a faithful orientation handle after cross-source merges). That is a structural multi-step increment, not a one-liner. The d50048a workaround must stay until that lands.

  Original diagnosis (preserved for context):
  - The diamond re-union gap was previously attributed to maxima/through-vertex coincidence at the shared y=10 peaks / y=7.5 valleys. **That hypothesis was disproven.** Differential evidence:
  - Pairwise `Union(d0,d1)` = 287.5 and the 3-diamond union = 375 are both *correct* (match the MC oracle), even though both already contain shared y=10 peaks and y=7.5 valleys. The failure first appears at the **4th** cumulative union.
  - The failure **reproduces at full general position**: with diamond centres nudged to distinct Y levels (cy = 0, 0.5, 1.0, 1.5 — no two vertices share a scanline, no coincidences at all), the 4th cumulative Union is still wrong (engine 329 vs MC 462). Coincidence handling is therefore *not* the cause.
  - A throwaway differential harness over random *non-convex* simple-quad pairs (coords in [0,12]) reproduces it at ~2.5% of non-convex pairs, badly (e.g. engine 2.08 vs truth 51.84). This is the same class as the §12.11 "non-convex involved ~8.3% wrong" residual.

  **Minimal reproducer** (no shared scanlines): `A = (2,11),(2,0),(10,8),(5,4)` (a non-convex "M": two local minima at (2,0),(5,4)) ∪ `B = (4,0),(7,2),(11,6),(3,3)`. Engine Union = 7.70; truth = 33.28. Correct outer ring (shoelace = 33.3): `(2,0),(3.5,1.5),(4,0),(7,2),(11,6),(6.2,4.2),(10,8),(5,4),(2,11)`.

  **Mechanism.** A's diagonal edge (2,0)→(10,8) crosses B's left side **twice** — at (3.5,1.5) and (6.2,4.2) — so along the edge the boundary order is `(2,0),(3.5,1.5),(6.2,4.2),(10,8)`, and the edge is on the union boundary in *two disjoint intervals* (lower `(2,0)–(3.5,1.5)` and upper `(6.2,4.2)–(10,8)`) with an interior gap between the crossings (the middle is inside B). The engine correctly builds two ring fragments — ring0 (lower, via `AddLocalMaxPoly` merging A∪B at (3.5,1.5)) and ring3 (upper, created by the cross-polytype `AddLocalMinPoly` at the (6.2,4.2) crossing, `isNew=false`) — but **splices them crosswise**: the final ring contains the two *collinear, overlapping, opposite-direction* edges `(10,8)→(3.5,1.5)` and `(6.2,4.2)→(2,0)` (overlapping on x∈[3.5,6.2]) instead of `(2,0)→(3.5,1.5)` and `(6.2,4.2)→(10,8)`.

  **Locus (confirmed by the fix).** `IntersectEdges`'s decision tree is a faithful Clipper2 port (verified line-by-line against engine.cpp:1872-2084). The fault was *not* in the dispatch but in two AEL-position mutations performed at the wrong time relative to `getPrevHotEdge` (see the two numbered fixes above); the `AddLocalMaxPoly` `IsFront(e1)==IsFront(e2)` workaround was a *symptom* of the resulting mis-orientation, not the bug. Same theme as b3d5020 (concave-union orientation divergence).
- **Xor / Difference classification — SUPERSEDED (see the "Classification audit — 2026-05-20 (later session)" note above).** This bullet originally read: "the differential's residual failures are dominated by identity violations (`X≠U−I`, `D≠a−I`) even at general position (Xor ~15% @ 2 crossings, Difference ~10% @ 2 crossings)". That conclusion was an artifact of the older differential not separating degenerate from clean inputs and (for large coords) of MC oracle noise. The audit shows clean-input classification is 0.00% wrong for all four ops; there is no general-position Xor/Difference classification gap. The real residual is degenerate-position (track C).

**Symptom.** The boolean engine is reliable only for inputs whose boundaries do not properly cross (disjoint, nested, touching) or cross at axis-aligned / special-position points. For general-position sloped crossings it is broadly wrong.

**Evidence (differential test vs a Monte-Carlo area oracle, random *simple* CCW quad pairs, coords in [-1000,1000] so coincidences are negligible):**

| proper crossings between A and B | wrong-result rate |
|----------------------------------|-------------------|
| 0 (disjoint / nested)            | 0.3%              |
| 2                                | ~56%              |
| 4                                | ~68%              |

Convexity is nearly irrelevant (convex-convex ~49%, non-convex ~57%); the driver is the crossing count. The Monte-Carlo oracle is internally consistent (|A∪B|+|A∩B| ≈ |A|+|B|, |A⊕B| ≈ |A∪B|−|A∩B|) while the engine's areas are grossly off, so the fault is the engine, not the harness or fixed-point precision.

**Minimal reproducer** (two convex quads, 2 crossings, no shared vertices):

```
a = (1,1),(-1,2),(0,-2),(2,0)     area 5.5
b = (-2,1),(-5,0),(0,-3),(0,1)    area 11
engine: Union area 11 (drops a entirely), Intersect 0
truth:  Union ≈ 15.4,                      Intersect ≈ 1.13
```

A clip-level trace of this case shows **zero `EventIntersection`s processed** — the two crossings are never scheduled.

**Root cause.** The sweep schedules crossings *incrementally*: `maybeScheduleIntersect` runs only when two edges become AEL-adjacent through a specific event (bot/local-min spawn, cursor advance, a prior intersection's post-swap neighbour check, horizontal promotion). When the relevant adjacency forms through an AEL rearrangement that does not trigger a fresh pairwise check, the crossing is silently lost. The §12.6.1 horizontal fix and the post-horizontal `rescanAdjacentIntersections` patch (commit "reschedule crossings after horizontal pass") each closed one narrow instance, but the disease is general: incremental scheduling cannot reliably enumerate the crossings in a scanbeam.

**Fix: per-scanbeam intersection recompute (Clipper2's `DoIntersections`).** Stop scheduling crossings incrementally. Instead, for each scanbeam `(botY, topY)` — `botY` = the scanline just processed, `topY` = the next event scanline — recompute *all* crossings from the settled AEL and process them bottom-up:

1. Every AEL edge present at `botY` spans the whole beam (its `Top.Y ≥ topY`, because every vertex Y is an event and `topY` is the next one). So enumerate edge pairs and keep those whose `Intersect` is a `ProperCross` with `botY < pt.Y < topY`. (Correctness-first: O(n²) per beam, matching the existing O(n³) preprocess; a later optimisation can use Clipper2's merge-sort inversion counter, `BuildIntersectList`.)
2. Sort the crossing nodes by `(pt.Y, pt.X)`.
3. Process bottom-up. The lowest crossing's two edges are AEL-adjacent; call `IntersectEdges` (which swaps, re-classifies, and emits). If rounding leaves a node's edges non-adjacent, apply Clipper2's `ProcessIntersectList` edit: advance to the next node whose edges *are* adjacent and process it first (`engine.cpp` `ProcessIntersectList`/`EdgesAdjacentInAEL`).

Integration: in `run()`, call `doIntersections(prevY, y)` at the top of each scanline iteration (after the first), with the AEL still in `prevY` order, before `UpdateForScanline(y)` and the Top/LocalMin handlers. This subsumes both the incremental scheduler and the horizontal rescan, which are removed. `EventIntersection` is no longer enqueued.

**Out of scope for this increment / remaining caveats.** Degenerate positions (shared vertices, vertex-on-edge T-junctions, collinear overlaps that survive preprocess) are a separate hardening task; the differential harness should be re-run with a *generic-position* filter to confirm the crossing-count failure rates collapse, then separately with degeneracies allowed to size the remaining work.

  **Track C — heavy-OutRec-port premise REFUTED; residual re-rooted to a postprocess nesting bug (2026-05-21).** The prior session's conclusion — that the single-shared-vertex residual needs the heavier OutRec ownership/orientation model (port Clipper2 `SetOwner`/`Split`/`SwapFrontBackSides`) — was built on two false premises, both overturned empirically this session:

  1. **Clipper2's closed-path boolean never calls `SetOwner` or `SwapFrontBackSides`.** `SetOwner` is gated on `using_polytree_` (engine.cpp:1357,1404); `SwapFrontBackSides` is reached only for `IsOpenEnd` edges (engine.cpp:1387-1390) — the closed-path same-side case is a hard error (`succeeded_=false`). So those functions cannot be the missing orientation handle for closed-path polygon booleans. The closed-path front/back is maintained purely by `AddLocalMinPoly`'s `OutrecIsAscending(prevHotEdge)==is_new` + `JoinOutrecPaths` (both already ported) plus the `join_with`/`Split` collinear-merge (a separate later feature, not an orientation handle).

  2. **The "regression repro truth = 29.92" is unachievable by Clipper2 itself.** Built and ran real Clipper2 (`g++` + `clipper.engine.cpp`) on `Xor(A=(4,1),(12,10),(10,9),(9,12) ; B=(3,4),(7,5),(10,9),(2,9))`: it *succeeds* (never hits `succeeded_=false`) and returns Xor **30.5** — and is itself internally inconsistent (Union 38 while |A|+|B|−I = 18+26.5−7.5 = 37). A 200M-sample even-odd MC oracle gives Union 37.21 / Intersect 7.29 / Xor 29.92 (self-consistent). So on this degenerate input even the reference engine is ~0.79 wrong; the d50048a workaround's 29.924 actually matched MC *better* than Clipper2 does. Chasing 29.92 by porting more Clipper2 machinery is chasing a target Clipper2 can't hit.

  **Three-way differential (polyclip vs Clipper2 vs MC oracle), 6000 single-shared-vertex simple-CCW-integer-quad pairs, coords [0,12], TOL=0.15:**

  | op | polyclip wrong (before fix) | Clipper2 wrong |
  |----|------|------|
  | Union | 57 (0.95%) | 4764 (79.4%) |
  | Intersect | 1 (0.02%) | 4563 (76.1%) |
  | Difference | 18 (0.30%) | 4686 (78.1%) |
  | Xor | 31 (0.52%) | 4773 (79.6%) |

  Clipper2's ~78% is *mostly* small integer-snap error at the tiny [0,12] scale (it rounds crossings to the integer grid and drops ~1-unit slivers — e.g. it returns Intersect=0 where the true sliver is ~1.1; polyclip scales to a fine fixed-point grid internally and tracks MC within ~0.05). So polyclip is dramatically MORE accurate than Clipper2 on degenerate shared-vertex inputs — it has not "fallen short of the reference," it has surpassed it here. But polyclip's rare failures are *gross*, and on exactly those Clipper2 is correct.

  **Root cause of the gross polyclip failures — postprocess nesting, NOT the sweep.** Dumping the raw sweep rings (`assembleResult` input) for the worst repros showed the sweep emits **correct, separately-oriented CCW rings**; the failure is entirely in `assembleResult`'s hole/nesting classification. Two simple quads that touch at exactly one shared vertex (otherwise disjoint, so Union = |A|+|B|) were collapsed because the nested-outer demotion sampled the smaller ring's *vertex centroid* and tested boundary-inclusive `Polygon.Contains`. When the two rings share a vertex — and especially a collinear (e.g. vertical) shared edge — that centroid lands exactly ON the other ring's boundary, which `Contains` counts as inside, so one polygon is wrongly demoted to a HOLE of the other. Worked examples (all: sweep rings correct, postprocess wrong → fixed):
  - `Union((0,5),(3,2),(6,3),(6,6) ; (6,0),(8,9),(4,8),(6,6))` — shared (6,6), B's centroid x=6 on A's vertical edge x=6: Union 4.0 → 26.0 (truth 25.96).
  - `Union((0,4),(6,6),(6,10),(5,11) ; (2,2),(9,7),(6,10),(7,6))` — shared (6,10): 12.0 → 24.0.
  - `Union((12,8),(9,3),(0,6),(9,0) ; (8,4),(12,8),(7,10),(0,8))` — shared (12,8): 18.0 → 54.0.

  **Fix (`assembleResult`, boolean.go).** Nesting is now decided by sampling a *genuine interior point* of the inner ring (new `interiorPoint`: cast a horizontal ray through the ring's vertex-Y centroid, take the midpoint of the widest interior span — guaranteed strictly inside even for concave rings) and testing it against the outer. The sweep's output rings have pairwise-disjoint interiors, so an interior point of the inner ring is strictly inside the outer iff the inner ring is genuinely nested, and strictly outside iff the two rings only touch — eliminating both the touching-rings false positive AND the opposite false negative (a hole all of whose vertices lie ON the outer boundary, e.g. the Xor overlap rectangle whose corners sit on the union outline — which a vertex-based test would wrongly promote). Both the first-pass hole→outer ownership and the nested-outer demotion now use this predicate; `polyCentroid` is deleted. Guard: `TestBooleanSharedVertexNotNested`. Full suite green (incl. `TestXorOverlappingAxisAligned`, which exercises the genuine-hole-on-boundary case). The d50048a `AddLocalMaxPoly` workaround is a *sweep-level* path untouched by this fix; it is orthogonal to the gross nesting failures and remains as-is.

  **Effect (same 6000-pair differential, before to after):** Union 0.95% to 0.53% (57 to 32 wrong), Xor 0.52% to 0.17% (31 to 10), Intersect 0.02% and Difference 0.30% unchanged. The fix removed ~46 failing pairs — every one a *touching-but-disjoint* shared-vertex pair wrongly nested. Clipper2's rates are unchanged (~78%; it is unaffected by polyclip postprocess).

  **Residual after this fix is sweep-level, NOT postprocess.** The remaining ~32 gross Union failures are *overlapping* (not merely touching) shared-vertex pairs where the SWEEP emits a single malformed small ring — e.g. `Union((0,4),(7,2),(8,0),(10,4) ; (10,4),(11,6),(2,12),(7,1))` (shared (10,4), they properly overlap) returns one ring of area 14.4 vs truth ~43.9; dumping shows a mis-merged ring, not a nesting error. This is the genuine d50048a crossing-orientation residual (the prior session's `AddLocalMinPoly` reflex-min / cross-source-respawn class), now cleanly separated from the postprocess-nesting class. **Next increment** targets that sweep-level mis-merge directly — validated against a MC oracle (NOT against Clipper2, which is *less* accurate than polyclip on these tiny-integer degenerate inputs) via the throwaway three-way differential (`g++` Clipper2 + MC + Go-emitted polyclip areas over single-shared-vertex CCW-quad pairs, [0,12], rebuilt each session).

  **Sweep-level mis-merge — FIXED (2026-05-21, this session): the at-vertex max→through-vertex handoff (`handoffMaxThroughVertex`).** The (10,4) repro above was traced and the root cause was NOT the prior session's `AddLocalMinPoly` reflex-min parity diagnosis — it is a missing crossing dispatch at the shared vertex. Mechanism: A's right edge (8,0)→(10,4) reaches the shared vertex (10,4) HOT (it is `or0`'s front) at A's local-max plateau; B's right bound passes THROUGH (10,4) (a through-vertex: edge (7,1)→(10,4) continuing to (10,4)→(11,6)) but is COLD — it entered A's interior at a lower crossing and exits exactly at the shared vertex. The union boundary must hand off from A's terminating right edge onto B's continuing edge (10,4)→(11,6) (which is outside A, hence boundary). But that handoff is invisible to every existing path: `doIntersections` sees only a Touch at the beam boundary (A's edge ends at (10,4), not a ProperCross strictly inside the open beam); `reconcileSharedVertexCrossings` sees no AEL inversion (A's edge does not continue above (10,4)); and `closeBound` removed A's maximum (Case A coupled-handoff) WITHOUT crossing the on-column through edge, because `maximaPartner` returned nil — A's true plateau partner (its horizontal top) is unreachable, blocked in the AEL by B's *left* bound which crosses the plateau at (5.64,4), OFF the apex column. So B's right bound stayed cold all the way up and B's entire upper triangle (→(11,6)→(2,12)) was dropped, collapsing the union to one 14.4-area ring.

  **Fix:** `handoffMaxThroughVertex`, called at the top of `closeBound`. While the closing edge `ae` is hot, cross any AEL-adjacent edge that (a) is on the apex column (`XAtY == maxPt.X`), (b) is NOT `ae`'s maxima partner, (c) continues above `maxPt` (`!IsBoundLast`), and **(d) is COLD**. `IntersectEdges` then transfers `ae`'s hot ring onto the through edge (the e1-hot/e2-cold branch: `AddOutPt` + `SwapOutrecs`), so the through edge becomes hot and carries the ring up; `ae`, now cold, closes as a trivial (non-contributing) maximum. The repro yields the exact correct ring (area 43.88, the full 9-vertex union outline). A `crossed` set guards against re-crossing the same neighbour after the AEL swap.

  **The COLD guard (d) is essential** and was added after the first version regressed Difference/Xor: when the through edge is HOT it already carries its own ring on both sides of the vertex, and crossing it double-handles — surfacing as a same-side `AddLocalMaxPoly` (the d50048a workaround branch) and a tangled output ring (e.g. `Xor((7,6),(8,5),(10,4),(8,6) ; (8,6),(11,8),(9,9),(6,3))`, shared (8,6), regressed to 2.43 vs truth 6.067). Restricting the handoff to cold through-edges fixes all three first-pass regressions; such hot-edge confluences are left to the maxima / between-maxima logic. The handoff only fires at exact on-column integer coincidences, so general position is untouched by construction.

  **Effect (4000 single-shared-vertex CCW-quad pairs [0,12] vs MC oracle, tol 0.15, before→after):** Union 15→8 wrong; **Intersect 0, Difference 10, Xor 2 all unchanged** — a clean win, zero regressions (verified by a per-pair before/after diff). Guard: `TestUnionOverlappingSharedVertexMismerge`. Full suite green. The d50048a `AddLocalMaxPoly` same-side workaround still exists and still fires for the *hot-through-edge* confluence class (the remaining Union/Difference/Xor residual); that class — a genuine 4-bound shared-apex confluence where A's plateau and B's max/through both meet — is the next increment, and is the structural maxima/ownership work the prior sessions flagged. **Validate next increment against the MC oracle, not Clipper2** (polyclip is more accurate than Clipper2 on these tiny-integer degenerate inputs; see the three-way differential above).
