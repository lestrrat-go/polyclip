# Conventions and gotchas

This document collects the rules `polyclip` expects you to follow and the
points where intuition from other libraries can mislead you. None of these are
deep — they are the small things that cause an hour of confusion if you do not
know them up front.

## Conventions

### Do not repeat the closing point

A ring lists each corner exactly once; the edge from the last point back to the
first is implicit. A square is four points, not five. Repeating the first point
at the end describes a zero-length edge, which is a defect, not a closed shape.
See [01-geometry-primitives.md](01-geometry-primitives.md).

### Winding distinguishes outers from holes

Outer rings are counter-clockwise; holes are clockwise. This is how the library
tells a boundary from a hole. Boolean operations accept either winding on input
and normalise internally, but the convention governs how output is returned and
how the public API interprets the rings you hand it. See
[01-geometry-primitives.md](01-geometry-primitives.md) and
[02-shapes-with-holes.md](02-shapes-with-holes.md).

### Boundary points count as inside

When testing whether a point lies in a region, a point lying exactly on a
boundary edge is treated as **inside**, for both outer rings and hole edges.
This makes containment well-defined for points on the boundary rather than
leaving them to the mercy of rounding. See
[02-shapes-with-holes.md](02-shapes-with-holes.md).

### Units are yours, but be consistent

The library never interprets your coordinates. Use one consistent unit
throughout an operation, and express offset distances and tolerances in that
same unit. See [00-anatomy.md](00-anatomy.md).

## Gotchas

### MultiPolygon is the only currency

Every operation takes and returns a `MultiPolygon`, even when your input is
conceptually a single shape. Wrap a lone shape in a `MultiPolygon`, and expect
a `MultiPolygon` back — possibly with more or fewer pieces than you started
with. There is no smaller "just one polygon" entry point, because results are
not closed over the smaller types. See [00-anatomy.md](00-anatomy.md).

### Construction does not validate

Building an `ExPolygon` or `MultiPolygon` by hand performs no checks. Holes
that escape their outer, holes that overlap, wrong windings, self-intersecting
rings — all are accepted at construction and only surface later, either as a
surprising result or when you explicitly run validation. If you are unsure
about hand-built input, validate it. See
[06-validation-and-cleaning.md](06-validation-and-cleaning.md).

### Cleaning cannot fix self-intersection

`Clean` removes redundant points; it does not run the engine and cannot resolve
crossing edges. To simplify a self-intersecting ring, send it through a boolean
operation instead. See [06-validation-and-cleaning.md](06-validation-and-cleaning.md).

### Coordinates are snapped to a grid

The engine works on an internal integer grid sized to your input's bounding
box. Points finer than one grid cell apart may merge. For geometry whose detail
is reasonable relative to its overall extent this is invisible; for extreme
ranges it is worth knowing. See
[05-numeric-robustness.md](05-numeric-robustness.md).

### Errors are reserved for genuine obstacles

The operations return an `error` only when they cannot produce a meaningful
result. An inward offset that erases the entire shape reports an
"empty result" error rather than returning nothing silently. A boolean
operation reports an "unsupported horizontal" error in the narrow case where a
particular arrangement of shared vertices and horizontal edges defeats the
engine's reconstruction. An empty or disjoint result, by contrast, is *not* an
error — it is a perfectly valid empty or unchanged `MultiPolygon`.
