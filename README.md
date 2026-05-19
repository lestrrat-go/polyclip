# polyclip

[![Go Reference](https://pkg.go.dev/badge/github.com/lestrrat-go/polyclip.svg)](https://pkg.go.dev/github.com/lestrrat-go/polyclip)

`polyclip` is a pure-Go library for 2D polygon operations: boolean ops
(union, intersection, difference, XOR) and polygon offsetting (Minkowski
sum / erosion with a disk). It is intended as a slicer-grade replacement
for the older Vatti ports in the Go ecosystem, comparable in scope to
[Clipper2](https://github.com/AngusJohnson/Clipper2).

**Status:** under construction. See [`DESIGN.md`](DESIGN.md) for the full
design and phased implementation plan.

## Install

```
go get github.com/lestrrat-go/polyclip
```

## Quick taste

```go
import "github.com/lestrrat-go/polyclip"

a := polyclip.MultiPolygon{{Outer: polyclip.Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}}}}
b := polyclip.MultiPolygon{{Outer: polyclip.Polygon{{5, 5}, {15, 5}, {15, 15}, {5, 15}}}}

u, err := polyclip.Union(a, b)
// ...
_ = u
_ = err
```

## License

MIT. See [`LICENSE`](LICENSE).
