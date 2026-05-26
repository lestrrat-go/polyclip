# polyclip

[![Go Reference](https://pkg.go.dev/badge/github.com/lestrrat-go/polyclip.svg)](https://pkg.go.dev/github.com/lestrrat-go/polyclip)

`polyclip` is a pure-Go library for 2D polygon operations: boolean ops
(union, intersection, difference, XOR), polygon offsetting (Minkowski
sum / erosion with a disk), and the surrounding toolbox a slicer needs.
It is a slicer-grade replacement for the older Vatti ports in the Go
ecosystem.

The engine is a Vatti scanline that works internally on an exact
fixed-point integer grid for numeric robustness, so it handles the cases
naive float clippers choke on: concentric circles, self-touching polygons,
collinear and coincident edges, and near-degenerate slivers.

- **Pure Go**, no cgo. The library itself has **no dependencies** beyond the
  standard library (testify is used only by the test suite).
- **Closed API:** every operation takes a `MultiPolygon` and returns a
  `MultiPolygon` — no separate post-processing step to make the result usable.
- **Robust** on adversarial input; correctness is held to a Monte-Carlo
  differential oracle plus fuzzing (see [`DESIGN.md`](DESIGN.md) §6).

## Status

The planar-polygon feature surface — boolean ops, offsetting, and the toolbox
above — is complete. The API
under the top-level `polyclip` package is the stable public surface; packages
under `internal/` are implementation detail and may change without notice.
See [`DESIGN.md`](DESIGN.md) for the full design rationale and engine internals.

## Install

```
go get github.com/lestrrat-go/polyclip
```

Requires Go 1.26 or later.

## Features

| Area | API |
|------|-----|
| Boolean ops | `Union`, `Intersect`, `Difference`, `Xor`, `UnionAll` |
| Self-intersection cleanup | `Simplify` |
| Offset (closed regions) | `Offset` with miter / round / square / bevel joins |
| Offset (open polylines → ribbons) | `OffsetPaths` with butt / square / round / joined end caps |
| Minkowski | `MinkowskiSum`, `MinkowskiDiff` |
| Fast axis-aligned clip | `RectClip`, `RectClipLines` |
| Path reduction | `SimplifyPaths` (Douglas–Peucker), `Clean` (dedup / tiny-feature removal) |
| Triangulation | `Triangulate` |
| Validation | `Validate` (structural diagnostics) |
| Advanced (`Builder`) | open-path clipping, selectable fill rules (incl. even-odd), nested `PolyTree` output, Z-coordinate tracking |

## Conventions

- A `Polygon` is a ring of points; the closing edge from the last point back
  to the first is **implicit** — do not repeat the first point.
- Outer rings are counter-clockwise, holes clockwise. Both orientations are
  accepted on input and normalized internally.
- Inputs are `float64` in user units; the engine snaps to a fixed-point grid
  internally (`DESIGN.md` §5).

## Quick taste

### Building shapes

```go
import "github.com/lestrrat-go/polyclip/geom"

// A square with a triangular hole, plus a second disjoint piece.
m := geom.New().
	Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).
	Hole(geom.Polygon{{3, 3}, {6, 3}, {5, 6}}).
	NextPiece().
	Point(20, 0).Point(24, 0).Point(22, 4).
	MustBuild()
_ = m
```

`geom.New()` builds a `MultiPolygon` fluently: `Point` extends the current
piece's outer ring, `Hole` attaches one or more `Polygon` rings as holes
(pre-built, literal, or spread from a `[]Polygon`), `NextPiece` starts a
disjoint piece, and `Build`/`MustBuild` normalizes winding (outer CCW, holes
CW). A ring can itself be built fluently and extracted with `MustPolygon` —
`geom.New().Point(…)…​.MustPolygon()` — then passed to `Hole`. You can also
construct the value types directly as literals — see below.

### Boolean ops

```go
import (
	"github.com/lestrrat-go/polyclip"
	"github.com/lestrrat-go/polyclip/geom"
)

a := geom.MultiPolygon{{Outer: geom.Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}}}}
b := geom.MultiPolygon{{Outer: geom.Polygon{{5, 5}, {15, 5}, {15, 15}, {5, 15}}}}

u, err := polyclip.Union(a, b)        // a ∪ b
i, err := polyclip.Intersect(a, b)    // a ∩ b
d, err := polyclip.Difference(a, b)   // a ∖ b
x, err := polyclip.Xor(a, b)          // a △ b
_, _, _, _, _ = u, i, d, x, err
```

### Offset

```go
// Inflate by 2 units with rounded corners; a negative distance shrinks.
out, err := polyclip.Offset(a, 2, polyclip.OffsetOptions{Join: polyclip.JoinRound})
_, _ = out, err
```

### Builder (fill rules, open paths, nested output)

```go
res, err := polyclip.New().
	AddSubject(a).
	AddClip(b).
	Fill(polyclip.FillEvenOdd).
	Execute(polyclip.OpUnion)
_, _ = res.Closed, err // res.Open carries any clipped open subjects
```

Runnable programs live in [`examples/`](examples).

## License

MIT. See [`LICENSE`](LICENSE).

The sweep engine's algorithm and data model are derived from
[Clipper2](https://github.com/AngusJohnson/Clipper2) by Angus Johnson
(Boost Software License 1.0); see [`NOTICE`](NOTICE) for attribution.
