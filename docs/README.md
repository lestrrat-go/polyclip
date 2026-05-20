# polyclip documentation

This directory explains the concepts and terminology behind `polyclip`. It is
a conceptual guide, not an API reference — it describes *what* the pieces are
and *what the words mean*, so that the package documentation on
[pkg.go.dev](https://pkg.go.dev/github.com/lestrrat-go/polyclip) reads clearly
once you know the vocabulary.

The documents are meant to be read in order, but each one stands on its own if
you already know the basics.

| Document | What it covers |
|----------|----------------|
| [00-anatomy.md](00-anatomy.md) | The shape of the library: the type ladder from `Point` to `MultiPolygon`, and the single principle that ties every operation together. |
| [01-geometry-primitives.md](01-geometry-primitives.md) | Points, bounding boxes, rings, the implicit closing edge, winding/orientation, and signed area. |
| [02-shapes-with-holes.md](02-shapes-with-holes.md) | How a region with holes is represented, how holes nest, and what "inside" means. |
| [03-boolean-operations.md](03-boolean-operations.md) | Union, intersection, difference, and symmetric difference as set operations on regions. |
| [04-offset.md](04-offset.md) | Growing and shrinking a region, and the choices that control how corners are drawn. |
| [05-numeric-robustness.md](05-numeric-robustness.md) | Why the engine works on an integer grid and what that means for your coordinates. |
| [06-validation-and-cleaning.md](06-validation-and-cleaning.md) | Checking input for structural problems and tidying cosmetic artifacts. |
| [07-conventions-and-gotchas.md](07-conventions-and-gotchas.md) | The rules the library expects you to follow, and the surprises to avoid. |
| [08-glossary.md](08-glossary.md) | Every term in one place, including the engine vocabulary you will meet in `DESIGN.md`. |

For the internal design of the boolean engine — the scanline sweep, the active
edge list, the bound model — see [`../DESIGN.md`](../DESIGN.md). The glossary
bridges the two: terms used only inside the engine are marked as such.
