# Retiring the offset self-union vote: SoS / symbolic-perturbation investigation

Status: investigation only (no engine change). Companion to DESIGN.md §7.1–§7.2.

## 1. The problem, exactly

`Offset`'s inward-offset topology resolution (§7.1) runs a positive-fill
self-union over a self-overlapping raw offset ring. The blocker to doing it in
a single sweep (DESIGN §7.2 "L3") is **exactly coincident parallel walls**.

Canonical case — dumbbell `Offset(-2, JoinMiter)`. The raw offset ring (in
exact user coordinates, *before* any grid snap) is

```
(2,2)(8,2)(8,6)(22,6)(22,2)(28,2)(28,8)(22,8)(22,4)(8,4)(8,8)(2,8)
```

The segments `(22,6)→(22,2)` and `(22,8)→(22,4)` lie on the line `x=22` and
**overlap exactly** over `y∈[4,6]` (same for `x=8`). This is *not* a snapping
artifact — it is exact in the input: the neck `[10,20]×[4,6]` eroded to nothing
and its right wall collapsed onto the right pad's left wall at `x=22`. Both
walls descend (same `WindDx`), so left→right the winding is `-1 → 0 → +1`
(neck → infinitesimal sliver → right pad).

Two facts make this a true degeneracy for the sweep:

1. The two walls are **parallel and coincident**, so they never produce a
   crossing event — nothing ever orders them in the AEL.
2. The winding is correct either way (the `+1` pad is right of both walls
   regardless of order), but **ring assembly connectivity is not**: whichever
   wall is taken as the `+1` boundary is connected to the neck top/bottom
   horizontals, and the wrong choice pulls the (empty) neck into the output
   ring. Observed results across tie-break choices: 36 (correct), 64 (neck
   merged in), 16 (mis-connected).

So the open question is *not* "what is the winding" but "which of two
geometrically-identical edges does the output ring follow." Geometry alone
cannot answer it; the answer lives in the pre-collapse geometry.

The current shipped fix (§7.1) is a **multi-frame rotation vote**: rotate the
ring by several angles, run the sweep in each, keep the majority result.
Rotation is a perturbation that separates the coincident walls into distinct,
near-transversal lines, and the *true* relative order survives across frames
while degenerate mis-resolutions scatter.

## 2. Simulation of Simplicity (Edelsbrunner & Mücke, 1990)

SoS treats input point `i`'s coordinate `j` as perturbed by a distinct
infinitesimal `ε(i,j) = ε^(2^(i·d+j))`, a hierarchy in which every perturbation
dominates the sum of all higher-index ones. No two perturbed points are ever
collinear/coincident/cocircular. Each geometric **predicate** (sign of an
orientation / on-segment / overlap determinant) is evaluated symbolically:
when the exact determinant is `0`, expand in `ε` and return the sign of the
lowest-order non-vanishing term — computed as a fixed cascade of sub-minors
with known signs. The net effect: every degenerate predicate gets a
deterministic non-zero answer, identical to the answer for one fixed
infinitesimal perturbation. The algorithm never hits a tie, and the result is
guaranteed to be a valid output **for that perturbed input**.

Where it would attach in this engine: the tie cases this engine currently
resolves ad-hoc — `aelLess` (equal `CurrX` + equal slope), `Orient2D == 0`
(collinearity / T-junction), exact point equality, `collinearOverlap`. SoS
would replace those equality tie-breaks with index-based symbolic comparison,
so the two `x=22` walls (different source-vertex indices ⇒ different `ε`) get a
single deterministic order. One sweep, no vote.

## 3. The catch: *consistent* ≠ *intended*

SoS guarantees a result correct for **some** infinitesimal perturbation, and
that perturbation is fixed by **input index order**, which carries no geometric
meaning. For the dumbbell the order of the two `x=22` walls would be decided by
their positions in the offset ring's vertex list — there is no reason that
order matches "real pad wall outside, collapsed-neck fold inside." So
index-based SoS would **retire the vote and give full determinism/robustness,
but would not guarantee the geometrically intended erosion result** at a
snap/collapse-induced coincidence. It can deterministically produce the 64-area
"neck merged" answer just as easily as the 36-area correct one.

This is the crucial difference from the rotation vote: rotation is a
**geometry-respecting** perturbation (it preserves the pre-collapse relative
positions of the walls), whereas generic SoS is an **index-respecting** one.

## 4. Better-targeted options

1. **Geometry-aware normal perturbation (recommended prototype).** Before the
   self-union sweep, displace each offset edge by a deterministic infinitesimal
   along its own outward normal (the offset direction). This separates the two
   coincident walls in the *correct* order — the pad wall stays outside, the
   collapsed-neck fold moves strictly inside — so a single sweep yields the
   intended erosion. It is SoS-style (a directed symbolic perturbation) but
   encodes offset semantics, i.e. "one rotation-vote frame, chosen correctly
   instead of voted." Deterministic, single sweep, no majority logic. Plugs
   into the already-landed ordered path (`SweepRingsFill`).

2. **Full index-based SoS.** Heaviest (symbolic re-derivation of every sweep
   predicate) and, per §3, does not by itself guarantee the intended result.
   Not recommended as a first move; revisit only if a fully general
   degeneracy-proof engine is wanted for the boolean ops too.

3. **Keep the vote.** It is correct and the cost (~8 sweeps per pinched piece)
   is acceptable for a correctness-first library. The honest default.

Note that "compute on a finer grid" does **not** help here: the coincidence is
exact in the input geometry, not a product of integer snapping, so a finer grid
leaves the walls coincident.

## 5. Recommended next step

Prototype option (1) on the landed ordered path: give each offset ring edge a
deterministic outward-normal nudge of one grid unit (or a sub-grid ε in a
scaled-up frame) before `SweepRingsFill`, and check whether the dumbbell and
its rotations resolve to two 36-area islands at a single sweep, plus the
existing `TestOffsetInwardErosionOracle` random cases. If it holds, wire
`Offset` to the single-sweep ordered path and retire `selfUnionResolveAngles`.
If edges that are coincident but offset in the *same* direction appear (a true
ambiguity even for normal perturbation), fall back to keeping the vote.
