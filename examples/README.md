# Examples

Runnable, output-verified examples for `github.com/lestrrat-go/polyclip`. Each is
a Go [testable example](https://pkg.go.dev/testing#hdr-Examples): it appears in
the package's [pkg.go.dev](https://pkg.go.dev/github.com/lestrrat-go/polyclip)
documentation and its `// Output:` block is checked by `go test`, so the examples
cannot silently drift from the code.

Run them all:

```
go test ./examples/
```

## Index

Start with **basics** for the data model, then **union** for reading results back
out; the rest cover one operation each.

| Example | What it shows |
|---------|---------------|
| [basics](./basics_example_test.go) | the data model — `MultiPolygon` / `ExPolygon` / `Polygon` / `Point`, conventions, building and reading shapes |
| [union](./union_example_test.go) | `Union`, and how to iterate the result and read its vertices |
| [intersect](./intersect_example_test.go) | `Intersect` — the shared region |
| [difference](./difference_example_test.go) | `Difference` — subtract a region, reading the resulting outer ring and hole |
| [xor](./xor_example_test.go) | `Xor` — the symmetric difference |
| [offset](./offset_example_test.go) | `Offset` outward / inward, and collapse to empty |
| [offsetpaths](./offsetpaths_example_test.go) | `OffsetPaths` — an open polyline into a ribbon |
| [minkowski](./minkowski_example_test.go) | `MinkowskiSum` — sweep a pattern along a path |
| [rectclip](./rectclip_example_test.go) | `RectClip` — fast axis-aligned rectangle clip |
| [simplify](./simplify_example_test.go) | `Simplify` — resolve a self-intersecting ring |
| [builder](./builder_example_test.go) | `Builder` — accumulate inputs and clip an open polyline (`Result.Open`) |
| [fillrule](./fillrule_example_test.go) | `Builder.Fill` — even-odd fill turning an overlap into a hole |
