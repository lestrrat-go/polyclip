# Validation and cleaning

Two utilities help you deal with imperfect input. **Validation** inspects a
`MultiPolygon` and reports structural problems without changing it.
**Cleaning** removes cosmetic clutter and returns a tidied copy. They serve
different purposes and are not substitutes for each other.

## Validation: diagnosing structure

Validation answers the question "is this shape built the way the library
expects?" It returns a list of issues; an empty list means no problems were
found. Crucially, **validation issues are diagnostics, not errors**. A boolean
operation will still run on input that fails validation — the issues simply
warn you that the result might be surprising, or that you have built a
structure that does not mean what you intended.

The structural problems it looks for are:

- **Too few vertices** — a "ring" with fewer than three points. It encloses no
  area and is silently ignored by the engine.
- **Wrong winding** — an outer ring that is not counter-clockwise, or a hole
  that is not clockwise. The boolean engine normalises winding on its own, but
  the outer/hole distinction in the public API and in offset depends on the
  documented convention, so a wrongly-wound ring may be misclassified.
- **Self-intersecting** — a ring whose own edges cross each other. Boolean
  operations resolve self-intersections; offset assumes a simple ring.
- **Hole outside outer** — a hole with at least one vertex lying outside its
  containing outer ring, violating the containment requirement of an
  `ExPolygon` (see [02-shapes-with-holes.md](02-shapes-with-holes.md)).
- **Holes overlap** — two holes within the same `ExPolygon` whose interiors
  intersect, violating the non-overlap requirement.

Each reported issue carries enough location information — which piece, which
ring — to point you at the offending part of the input.

## Cleaning: removing cosmetic artifacts

Cleaning produces a copy of a `MultiPolygon` with three kinds of harmless-but-
untidy clutter removed:

- **Duplicate vertices** — consecutive points that sit on top of each other
  (within a tolerance you supply) are merged into one. The check wraps around
  the end of the ring, so a final point that lands back on the first is dropped
  too.
- **Collinear vertices** — an interior point that lies on (or very near) the
  straight line between its two neighbours adds nothing to the shape and is
  removed. This pass repeats until no more can be removed, so a long run of
  collinear points collapses to a single edge.
- **Tiny rings** — a ring enclosing less area than a threshold you supply is
  dropped. If an outer ring is dropped, its whole piece goes with it; dropping
  a hole leaves the outer intact.

You control two tolerances: a vertex-distance tolerance for the merge and
collinearity passes, and a minimum area for the tiny-ring pass. Passing zero
disables a check — with a zero vertex tolerance, only *exactly* coincident
points are merged.

## What cleaning is not

Cleaning is **purely geometric**. It walks the rings and removes redundant
points; it does *not* run the boolean engine. It therefore **cannot resolve a
self-intersection** — it has no machinery to compute where edges cross and
re-stitch the result. To turn a self-intersecting ring into a clean simple
region, run it through a boolean operation (a union of the shape with itself
resolves the crossings under the engine's rules). Cleaning is for tidying an
already-valid shape; the engine is for fixing a malformed one.
