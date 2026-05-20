# Geometry primitives

This document explains the smallest building blocks: points, bounding boxes,
rings, and the two properties of a ring that the library cares about most —
its *winding* and its *signed area*.

## Point

A **point** is a single location in the plane, given by an `X` and a `Y`
coordinate. Both are `float64`. A point has no size and no direction; it is
just a place.

Points are the atoms of every shape. A ring is a sequence of points; a region
is a collection of rings.

## Bounding box

A **bounding box** (`BBox`) is the smallest
[axis-aligned](https://en.wikipedia.org/wiki/Minimum_bounding_box) rectangle
that contains a set of points. "Axis-aligned" means its sides are parallel to
the X and Y axes — it is described completely by two corners, a minimum and a
maximum.

Bounding boxes are cheap to compute and cheap to compare, so they are used as a
first, coarse test before any expensive geometry. If two shapes' bounding boxes
do not even touch, the shapes themselves cannot overlap, and an operation can
take a shortcut without examining a single edge.

The library distinguishes the **empty** bounding box — one that contains no
points at all — from a box that happens to contain a single point at the
origin. The empty box is a deliberate sentinel: extending it with a point
yields exactly that point, so it is the correct starting value when you build
up a box from a stream of points.

## Ring

A **ring** is a closed loop of points — a `Polygon` in the library's
vocabulary. It traces the boundary of a region by listing the corner points in
order.

A ring is [**simple**](https://en.wikipedia.org/wiki/Simple_polygon) if its
edges do not cross one another. A ring that does
cross itself (a figure-eight, for instance) is **self-intersecting**. The
library accepts self-intersecting rings as input to boolean operations, which
resolve the crossings; other parts of the library assume simple rings.

### The implicit closing edge

A ring is *closed*, meaning the last point connects back to the first to
complete the loop. `polyclip` makes this closing edge **implicit**: you list
each corner exactly once and do *not* repeat the first point at the end.

A square is therefore four points, not five. If you do repeat the first point,
you have described a ring with a zero-length final edge, which is a structural
defect rather than a closed square.

This is one of the most common sources of confusion when moving from another
library, so it is worth stating plainly: **list each vertex once; the loop
closes itself.**

## Winding and orientation

The points of a ring are listed in an order, and that order has a direction —
you can trace the boundary either **counter-clockwise** (CCW) or
**clockwise** (CW). This direction is the ring's **winding** or
[**orientation**](https://en.wikipedia.org/wiki/Curve_orientation).

Winding is not a cosmetic detail. It is how the library tells the *outside* of
a ring from the *inside*, and how it tells an outer boundary from a hole. The
convention `polyclip` uses is:

- **Outer rings wind counter-clockwise.**
- **Holes wind clockwise.**

(This assumes the conventional screen orientation where Y increases upward. If
your Y axis points downward, the visual sense of "clockwise" flips, but the
library's rule is always stated in terms of signed area, below, which is
unambiguous.)

You are not required to supply input in this winding — the boolean operations
normalise it internally and accept either orientation. But the convention
governs how output is returned and how holes are distinguished, so it is the
mental model to keep.

## Signed area

The **signed area** of a ring is its area carrying a plus-or-minus sign that
encodes the winding:

- A **positive** signed area means the ring is wound counter-clockwise.
- A **negative** signed area means it is wound clockwise.
- A signed area of **zero** means the ring is degenerate — it has fewer than
  three distinct points, or all its points are collinear, so it encloses no
  area at all.

The magnitude is the ordinary geometric area; the sign is the orientation.
Signed area is computed by the [*shoelace formula*](https://en.wikipedia.org/wiki/Shoelace_formula), which
sums a cross-product term over consecutive vertices. Because a
single number reports both how much area a ring encloses and which way it
winds, signed area is the library's workhorse for classifying rings — most
importantly, for telling an outer ring (positive) from a hole (negative).
