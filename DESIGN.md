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
├── offset/                    internal offset engine (subpackage)
│   ├── doc.go
│   ├── edge.go                per-edge offset rectangles + join geometry
│   └── arc.go                 arc tessellation for round joins
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

Subpackages under `clip/`, `offset/`, `fixed/` are **internal in spirit** but kept exported within the module so tests can address them directly. They are not part of the stable public API; the only stable surface is what's exported by the top-level `polyclip` package.

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

Offset is **not** the same algorithm as boolean. It works like this:

1. For each edge of the input polygon, construct a rectangle that the edge sweeps when moved outward by `d`. (For inward offset, the rectangle is on the other side.)
2. At each vertex, construct a **join geometry** — miter, round, or square — that connects the two adjacent rectangles.
3. The collection of these rectangles + joins is a (possibly self-overlapping) polygon. Take its **union with itself** via the boolean engine. The result is the offset polygon.

Equivalent phrasing: the offset region is the Minkowski sum of the polygon with a disk (or square, depending on join type). Doing it via "fat-edge polygons → union" is the standard implementation, and it's how Clipper2's offset module works.

Why this approach over a direct edge-walk + miter math like makislicer's current naive offset?

- **Handles topology change for free.** When inward offset makes a feature collapse, the corresponding edge-rectangles produce zero or negative-area regions that the union eliminates.
- **Disjoint output for free.** A U-shape offset inward enough to split into two pieces — the union correctly produces two output polygons.
- **Correctness at sharp reflex corners.** No special cases.

The cost is that offset depends on the boolean engine being implemented first. Implementation steps for `offset/`:

- `offset/edge.go` — given an input ring, produce the list of per-edge offset rectangles and per-vertex join polygons (without yet unioning).
- `offset/arc.go` — for round joins, tessellate the arc into segments respecting `ArcTol`.
- `offset.go` (top-level) — orchestrate: build the per-edge fragments for outer + holes, feed into `Union`, return.

### 4.4 Algorithmic complexity

- Boolean ops: `O((n + k) log n)` where `n` = total edges and `k` = total intersection points. For typical slicer layers `k = O(n)`, so `O(n log n)`.
- Offset: dominated by the union of `O(n)` rectangles, so also `O(n log n)`.

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
- `offset/edge_test.go` — per-edge fragment geometry for a single edge.

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

### Phase 2 — Boolean engine: union only (≤ 1500 LoC)
- [ ] `clip/segment.go`, `clip/sweep.go`, `clip/intersect.go`, `clip/classify.go` (union table only), `clip/build.go`.
- [ ] Public `Union(a, b MultiPolygon)`. `UnionAll` defined naively as repeated `Union` for now.
- [ ] All adversarial-case tests for union pass.
- [ ] Fuzz seed corpus committed.
- **Exit criterion**: `Union` is robust enough to feed into the slicer's skin-detection prototype.

### Phase 3 — Other boolean ops (≤ 200 LoC delta)
- [ ] Extend the classification table in `clip/classify.go` for intersect, difference, xor.
- [ ] Public `Intersect`, `Difference`, `Xor`.
- [ ] Invariant tests from §6.2 pass.

### Phase 4 — Offset (≤ 800 LoC)
- [ ] `offset/edge.go`, `offset/arc.go`, `offset.go`.
- [ ] Miter and round joins. Square is trivial; add at the end.
- [ ] Round-trip property tests.

### Phase 5 — Quality & speed
- [ ] `UnionAll` tournament reduction for `O(n log n)` instead of repeated pairwise.
- [ ] `Clean()` implementation.
- [ ] `Validate()` implementation.
- [ ] Benchmarks; profile and optimise hot paths in the sweep.
- [ ] Documentation pass: every public symbol has a Go doc comment with at least one example.

### Phase 6 — Examples and v0.1 release
- [ ] `examples/union/`, `examples/offset/` runnable programs.
- [ ] Tag `v0.1.0`. Downstream `lestrrat-go/makislicer` switches off its naive offsetter onto polyclip.

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
- Internal subpackages (`clip/`, `offset/`, `fixed/`) may use shorter doc comments and may export aggressively for testing — they're not part of the stable API.
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

The simplest implementation: during preprocess, after the overlap split, scan the segment list and merge / drop coincident pairs *before* building the event queue.

### 11.8 Horizontal segments

A horizontal segment `h` has `Bot.Y == Top.Y` and lives at a single scanline `Y_h`. It contributes zero to `WindSelf` over any non-zero Y interval, so it never enters the AEL; instead, the engine processes horizontals via a dedicated **horizontal pass** at each scanline.

Event-queue contract for horizontals (changes to §11.5):

- A horizontal segment generates a single `EventHoriz` event at `(Y_h, Bot.X)` carrying the full segment.
- `EventKind` ordering at the same `(Y, X)` is: `Top < Horiz < Bot < Intersection`. Closing edges leave the AEL before horizontals walk through it; horizontals finish before new edges enter and before intersection swaps.

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

Useful as runtime asserts (controlled by a build tag) or as test post-conditions:

1. After each event the AEL is sorted left-to-right by `CurrX` with slope tie-break.
2. After each event no `WindSelf` exceeds the number of input rings of that source (sanity bound).
3. Every `ActiveEdge` with non-nil `Out` is classified contributing.
4. No two contributing same-source edges are adjacent in the AEL (would indicate a missed classification flip).
5. At sweep end, every `OutRing.Pt` is non-nil → every ring closes.

### 11.11 Implementation order for the remaining Phase 2 increments

Recommended commit boundaries from here:

- **Increment 4:** preprocess pipeline (`clip/preprocess.go`): scale, snap, drop degenerates, overlap split, coincident-edge dedup. Tests cover each transformation in isolation. No sweep yet — the function returns `[]Segment`.
- **Increment 5:** sweep loop skeleton (`clip/sweep.go`): event loop that processes Bot/Top/Intersection without yet computing classification or emitting output. Logs/records the sequence of events as an internal trace. Test asserts the trace for a couple of hand-built inputs.
- **Increment 6:** winding bookkeeping + classification table for Union (`clip/classify.go`). At this point `Contributing` is set correctly on every AEL edge; still no output emission. Test the trace including contribution flags.
- **Increment 7:** output ring assembly (`clip/build.go`) plus public `Union` (`boolean.go`). End-to-end works for the simplest adversarial cases.
- **Increment 8:** rest of the adversarial suite (concentric rings, coincident edges, self-touching 8s, T-junctions), fuzz seed corpus.

Each increment ships with its own tests; each is its own commit on the feature branch. Phase 2 fast-forwards to main when increment 8's adversarial suite passes.

