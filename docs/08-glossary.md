# Glossary

Every term in one place. Entries marked **(engine)** name internal mechanisms
you will only meet inside the `clip/` and `fixed/` subpackages or in
[`../DESIGN.md`](../DESIGN.md); you do not need them to use the library, but
they are defined here so the engine documentation reads clearly.

### Active edge list (AEL) **(engine)**
The set of polygon edges that the [sweep line](#sweep-line-scanline) currently
crosses, kept sorted left-to-right. As the sweep advances, edges enter and
leave this list, and neighbouring edges are tested for intersection. The core
data structure of the Vatti algorithm.

### Arc tolerance
The maximum distance a straight chord may deviate from the true circular arc it
approximates, when a round join is [tessellated](#tessellation) into segments.
Smaller tolerance, smoother curve, more segments. See
[04-offset.md](04-offset.md).

### Axis-aligned bounding box
See [bounding box](#bounding-box).
[Wikipedia](https://en.wikipedia.org/wiki/Minimum_bounding_box).

### Boolean operation
An operation that combines two regions with set logic — union, intersection,
difference, or symmetric difference. Named after
[Boolean algebra](https://en.wikipedia.org/wiki/Boolean_algebra). See
[03-boolean-operations.md](03-boolean-operations.md).

### Bound **(engine)**
A monotonic chain of edges running up one side of a polygon, from a
[local minimum](#local-minimum--local-maximum) to a local maximum. The Vatti
engine processes polygons as left and right bounds rather than as individual
edges. See [`../DESIGN.md`](../DESIGN.md).

### Bounding box
The smallest [axis-aligned](https://en.wikipedia.org/wiki/Minimum_bounding_box)
rectangle containing a set of points, described by a minimum and a maximum
corner. Used as a cheap first test before expensive geometry. See
[01-geometry-primitives.md](01-geometry-primitives.md).

### Chamfer
A flat cut across a corner, used in place of a sharp [miter](#miter) point when
the miter would extend past the [miter limit](#miter-limit). See
[04-offset.md](04-offset.md). Related:
[chamfer](https://en.wikipedia.org/wiki/Chamfer).

### Clockwise (CW)
A [winding](#winding--orientation) direction; the orientation `polyclip` uses
for holes. Has a negative [signed area](#signed-area).

### Coincident edges
Two edges lying on the same line, overlapping wholly or partly. They need
special handling because naive clipping produces zero-area artifacts at them.
See [05-numeric-robustness.md](05-numeric-robustness.md).

### Collinear
Points lying on a single straight line. A collinear interior vertex adds no
shape and is a candidate for removal during [cleaning](#cleaning).
[Wikipedia](https://en.wikipedia.org/wiki/Collinearity).

### Containment
Whether a point lies inside a region, decided by the
[even-odd rule](#even-odd-rule). Boundary points count as inside. See
[02-shapes-with-holes.md](02-shapes-with-holes.md).

### Convex / reflex corner
A [convex](https://en.wikipedia.org/wiki/Convex_polygon) corner turns the
boundary one way (outward); a *reflex* corner turns it the other (inward, a
"dent"). The distinction decides whether an outward offset opens a gap to be
filled by a [join](#join) or closes the edges onto each other. See
[04-offset.md](04-offset.md).

### Counter-clockwise (CCW)
A [winding](#winding--orientation) direction; the orientation `polyclip` uses
for outer rings. Has a positive [signed area](#signed-area).

### Cleaning
Removing cosmetic clutter from a shape — duplicate points, collinear vertices,
tiny rings — without running the engine. Cannot resolve self-intersection. See
[06-validation-and-cleaning.md](06-validation-and-cleaning.md).

### Difference
The boolean operation `a ∖ b`: points in `a` but not in `b`. Not symmetric. See
[03-boolean-operations.md](03-boolean-operations.md).
[Wikipedia](https://en.wikipedia.org/wiki/Complement_(set_theory)#Relative_complement).

### Degenerate
A shape that has collapsed below meaningful dimension — a ring with fewer than
three distinct points, or one whose points are all collinear, enclosing zero
area.

### Erosion
Shrinking a region inward, the [Minkowski](#minkowski-sum--erosion) erosion
performed by a negative offset distance. See [04-offset.md](04-offset.md).
[Wikipedia](https://en.wikipedia.org/wiki/Erosion_(morphology)).

### Even-odd rule
The rule for deciding inside from outside: a ray from the point crosses the
boundary an odd number of times if inside, even if outside. Handles holes
automatically. See [02-shapes-with-holes.md](02-shapes-with-holes.md).
[Wikipedia](https://en.wikipedia.org/wiki/Even%E2%80%93odd_rule).

### ExPolygon
"Extended polygon": one outer ring plus zero or more holes. The smallest type
that can describe a region with a hole. See
[02-shapes-with-holes.md](02-shapes-with-holes.md).

### Fixed-point grid
The internal integer grid the engine snaps coordinates onto for exact, robust
arithmetic. See [05-numeric-robustness.md](05-numeric-robustness.md).
Related: [fixed-point arithmetic](https://en.wikipedia.org/wiki/Fixed-point_arithmetic).

### Hole
An inner ring that subtracts area from its containing outer ring. Wound
[clockwise](#clockwise-cw). See [02-shapes-with-holes.md](02-shapes-with-holes.md).

### Intersection
The boolean operation `a ∩ b`: points in both `a` and `b`. See
[03-boolean-operations.md](03-boolean-operations.md).
[Wikipedia](https://en.wikipedia.org/wiki/Intersection_(set_theory)).

### Join
The shape used to fill the gap that opens at a corner during offset — a
[miter](#miter), a round arc, or a [square](#chamfer). See
[04-offset.md](04-offset.md).

### Local minimum / local maximum **(engine)**
The lowest and highest points of a [bound](#bound), where the
[sweep](#sweep-line-scanline) begins or ends processing a pair of edges. See
[`../DESIGN.md`](../DESIGN.md).

### Miter
A [join](#join) that extends both offset edges to their sharp intersection,
reproducing the original corner. [Wikipedia](https://en.wikipedia.org/wiki/Miter_joint).
See [04-offset.md](04-offset.md).

### Miter limit
A cap on how far a [miter](#miter) point may extend before it is replaced by a
[chamfer](#chamfer), expressed as a multiple of the offset distance. See
[04-offset.md](04-offset.md).

### Minkowski sum / erosion
The [Minkowski sum](https://en.wikipedia.org/wiki/Minkowski_addition) of a
region with a disk of radius `d` is everything within distance `d` of the
region — the geometric meaning of an outward offset. Erosion is the inverse,
the meaning of an inward offset. See [04-offset.md](04-offset.md).

### MultiPolygon
A set of disjoint [ExPolygon](#expolygon) values; the universal input and
output type of every operation. See [00-anatomy.md](00-anatomy.md).

### Offset
Growing or shrinking a region by a fixed signed distance. See
[04-offset.md](04-offset.md).

### Orientation
See [winding](#winding--orientation).

### Point
A single `(X, Y)` location in the plane, in the caller's units. See
[01-geometry-primitives.md](01-geometry-primitives.md).

### Polygon
A single closed ring of points. See
[01-geometry-primitives.md](01-geometry-primitives.md).
[Wikipedia](https://en.wikipedia.org/wiki/Polygon).

### Ring
A closed loop of points tracing a boundary. The closing edge is implicit. See
[01-geometry-primitives.md](01-geometry-primitives.md).

### Self-intersecting
A ring whose edges cross one another. Accepted by boolean operations (which
resolve the crossings); assumed absent by offset. See
[01-geometry-primitives.md](01-geometry-primitives.md).

### Shoelace formula
The formula for [signed area](#signed-area): a sum of cross-product terms over
consecutive vertices. Named for the criss-cross pattern of the multiplications.
[Wikipedia](https://en.wikipedia.org/wiki/Shoelace_formula).

### Signed area
A ring's area carrying a sign that encodes its [winding](#winding--orientation):
positive for counter-clockwise, negative for clockwise, zero if degenerate. See
[01-geometry-primitives.md](01-geometry-primitives.md).

### Simple polygon
A ring that does not cross itself.
[Wikipedia](https://en.wikipedia.org/wiki/Simple_polygon).

### Snapping
Moving a coordinate to the nearest point on the
[fixed-point grid](#fixed-point-grid). See
[05-numeric-robustness.md](05-numeric-robustness.md).

### Sweep line / scanline **(engine)**
An imaginary line swept across the plane that the Vatti algorithm uses to
process geometry in order; at each step it tracks which edges it crosses via the
[active edge list](#active-edge-list-ael). See
[`../DESIGN.md`](../DESIGN.md). Related:
[sweep line algorithm](https://en.wikipedia.org/wiki/Sweep_line_algorithm).

### Symmetric difference (Xor)
The boolean operation giving points in exactly one input: `(a ∪ b) ∖ (a ∩ b)`.
Exposed as *Xor*. See [03-boolean-operations.md](03-boolean-operations.md).
[Wikipedia](https://en.wikipedia.org/wiki/Symmetric_difference).

### Tessellation
Approximating a curve (such as a round join's arc) by a chain of straight
segments. Governed by [arc tolerance](#arc-tolerance).
[Wikipedia](https://en.wikipedia.org/wiki/Tessellation).

### T-junction
A point where a vertex of one ring lands exactly on the *edge* of another,
forming a "T". The engine splits the edge at that point so the topology stays
consistent. See [05-numeric-robustness.md](05-numeric-robustness.md).

### Union
The boolean operation `a ∪ b`: points in either input. See
[03-boolean-operations.md](03-boolean-operations.md).
[Wikipedia](https://en.wikipedia.org/wiki/Union_(set_theory)).

### Vatti algorithm **(engine)**
The polygon-clipping algorithm `polyclip`'s boolean engine implements, the same
family used by the Clipper libraries. It sweeps a line across the plane,
maintaining an [active edge list](#active-edge-list-ael) and classifying each
edge's contribution by [winding count](#winding-count). See
[`../DESIGN.md`](../DESIGN.md).
[Wikipedia](https://en.wikipedia.org/wiki/Vatti_clipping_algorithm).

### Winding / orientation
The direction a ring's points are listed — [counter-clockwise](#counter-clockwise-ccw)
or [clockwise](#clockwise-cw) — which distinguishes the inside from the outside
and outer rings from holes. See
[01-geometry-primitives.md](01-geometry-primitives.md).
[Wikipedia](https://en.wikipedia.org/wiki/Curve_orientation).

### Winding count **(engine)**
A running count, maintained per input as the [sweep](#sweep-line-scanline)
crosses edges, of how many times the region wraps around the current location.
The boolean classification rules read these counts to decide whether an edge
belongs in the output. See [`../DESIGN.md`](../DESIGN.md).
[Wikipedia](https://en.wikipedia.org/wiki/Winding_number).
