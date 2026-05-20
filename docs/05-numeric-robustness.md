# Numeric robustness

This document explains *why* the engine does not compute on your `float64`
coordinates directly, and what that choice means for you. You do not need any
of this to call the library, but it explains the one input limit worth knowing
about.

## The problem with floating point

Polygon clipping is built on a chain of decisions like "does this edge cross
that edge, and if so, exactly where?" Each crossing point becomes input to the
next decision. The trouble is that
[floating-point arithmetic](https://en.wikipedia.org/wiki/Floating-point_arithmetic)
is *inexact*: the same point, computed two different ways, can come out as two
very slightly different `float64` values.

For most arithmetic that tiny discrepancy is harmless. For a clipping engine it
is poison. The algorithm relies on points that *should* be identical actually
*being* identical — when they are not, two edges that should meet at a shared
vertex instead miss each other by a hair, the topology of the result becomes
inconsistent, and the output is garbled. This is the single most common way
naive polygon-clipping implementations fail.

## The fix: an integer grid

`polyclip` sidesteps the problem by not doing the delicate arithmetic in
`float64` at all. Before the engine runs, it **snaps** every input coordinate
onto a uniform grid of [integers](https://en.wikipedia.org/wiki/Integer)
(specifically 64-bit integers). All the engine's internal geometry then happens
on that grid, where arithmetic is *exact* — there is no rounding error, because
there are no fractions.

Two consequences make the engine robust:

- A point on the grid has exactly one representation. "The same point computed
  two ways" can no longer produce two different values, because both ways land
  on the same grid cell.
- The orientation tests — "is this point left of, right of, or exactly on this
  line?" — are computed with
  [exact integer arithmetic](https://en.wikipedia.org/wiki/Arbitrary-precision_arithmetic),
  using wide enough integers that the result is never rounded. The answer is
  always definitively one of the three, never a coin-flip between "barely left"
  and "barely right".

After the engine finishes, the integer results are converted back to `float64`
for you. The grid is an internal mechanism; your inputs and outputs are
ordinary floating-point coordinates.

## What this means for your coordinates

The grid has a finite resolution, and that resolution is chosen *per operation*
based on the [bounding box](https://en.wikipedia.org/wiki/Minimum_bounding_box)
of the inputs. The engine fits the grid to the data: a small shape gets a fine
grid, a large shape gets a coarser one, so that the exact-arithmetic guarantees
hold without the integers overflowing.

The practical effects:

- **Coordinates are snapped.** Two input points closer together than one grid
  cell collapse onto the same grid point. For well-separated geometry this is
  imperceptible; for inputs with detail far finer than the overall extent, it
  is the mechanism by which near-coincident points are unified.
- **Very large coordinate ranges trade away precision.** The wider the
  bounding box, the coarser the grid, and the more two nearby points may snap
  together. Keeping a shape's overall extent within a sane range of its finest
  meaningful detail avoids surprises.

You never set the grid resolution yourself; the engine derives it. The takeaway
is simply that `polyclip` is exact *on its grid*, and the grid is sized to your
data.
