# Shapes with holes

A single ring can only describe a solid shape. To describe a region with a
hole in it — a washer, a picture frame, the letter "O" — you need two layers
on top of the ring: the `ExPolygon` and the `MultiPolygon`.

## ExPolygon: a region with holes

An **`ExPolygon`** ("extended polygon") is one **outer** ring together with a
list of **hole** rings nested inside it. The region it represents is the area
enclosed by the outer ring, *minus* the areas enclosed by the holes.

The two roles are distinguished by winding (see
[01-geometry-primitives.md](01-geometry-primitives.md)): the outer ring winds
counter-clockwise, and each hole winds clockwise. The opposite windings are
what let the library treat the holes as subtractions from the outer rather than
as separate solid shapes.

### What the library expects of holes

For an `ExPolygon` to mean what it says, its holes must be:

- **Fully contained** in the outer ring — every part of a hole lies inside the
  outer boundary.
- **Non-overlapping** with each other — two holes do not share interior area.

These are requirements of the representation, not facts the library proves for
you when you construct an `ExPolygon` by hand. If you build one whose holes
poke outside the outer ring or overlap each other, the structure is malformed.
You can check for this with validation (see
[06-validation-and-cleaning.md](06-validation-and-cleaning.md)), and a boolean
operation always emits well-formed `ExPolygon` values regardless of how tangled
its input was.

## MultiPolygon: a set of disjoint regions

A **`MultiPolygon`** is a collection of `ExPolygon` values that do not overlap
one another. It describes a region that may consist of several separate
pieces — several islands, each of which may itself have holes.

`MultiPolygon` is the universal currency of the library: every boolean and
offset operation takes one and returns one (see
[00-anatomy.md](00-anatomy.md)). It is the only type general enough to express
any result an operation might produce, including the case where a single input
shape splits into several output pieces or collapses to none.

## Nesting and depth

Holes contain emptiness, but emptiness can itself contain solid. Imagine a
solid square, with a square hole in it, with a smaller solid square sitting
inside that hole. Each time you cross a boundary inward, you alternate between
*solid* and *empty*.

This alternation is captured by **nesting depth**:

- Depth 0 (outermost) — solid. This is an outer ring.
- Depth 1 — empty. This is a hole.
- Depth 2 — solid again. This is the outer ring of a separate `ExPolygon`
  nested inside the hole.
- ...and so on, alternating with each level.

Even depths are solid (outer rings); odd depths are empty (holes). When a
boolean operation produces nested rings, it uses this even/odd depth rule to
decide which rings are outers and which are holes, and to attach each hole to
the correct containing outer.

## What "inside" means: the even-odd rule

To decide whether a point lies inside a region, the library uses the
[**even-odd rule**](https://en.wikipedia.org/wiki/Even%E2%80%93odd_rule) (also
called the *parity rule*). Conceptually: draw a ray
from the point out to infinity and count how many ring edges it crosses.

- An **odd** number of crossings means the point is **inside** the region.
- An **even** number of crossings means it is **outside**.

This rule handles holes automatically. A point inside a hole sits inside the
outer ring (one crossing so far, odd, looks inside) but also inside the hole
ring (a second crossing, now even), so it is correctly reported as outside the
region. A point in the solid part inside a hole-within-a-hole crosses three
boundaries — odd again, inside.

A subtlety the library fixes by convention: a point lying *exactly on* a
boundary is ambiguous under a naive ray count. `polyclip` resolves this by
treating **boundary points as inside**. A point on an outer edge, or on the
edge of a hole, counts as belonging to the region.

## Derived quantities

Three properties fall directly out of this structure:

- **Area** — for an `ExPolygon`, the outer ring's area minus the sum of the
  holes' areas; for a `MultiPolygon`, the sum over all its pieces. Areas are
  reported as unsigned magnitudes.
- **Bounding box** — for an `ExPolygon`, the box of the outer ring alone (holes
  never extend past the outer, so they cannot enlarge it); for a
  `MultiPolygon`, the box covering every piece.
- **Containment** — whether a given point lies inside, decided by the even-odd
  rule applied through the whole nesting.
