package polyclip

import (
	"errors"

	"github.com/lestrrat-go/polyclip/clip"
)

// errUnknownOperation is returned by [Builder.Execute] for an Operation value
// outside the defined constants.
var errUnknownOperation = errors.New("polyclip: unknown operation")

// Operation selects the boolean operation run by [Builder.Execute]. It maps
// 1:1 to the engine's clip.Operation.
type Operation int

const (
	// OpUnion computes a ∪ b — the region covered by either input.
	OpUnion Operation = iota
	// OpIntersect computes a ∩ b — the region covered by both inputs.
	OpIntersect
	// OpDifference computes a ∖ b — the region covered by the subject(s) but
	// not the clip(s).
	OpDifference
	// OpXor computes the symmetric difference (a ∪ b) ∖ (a ∩ b) — the region
	// covered by exactly one input.
	OpXor
)

// Polyline is an open path: a sequence of points with no implicit closing
// edge. It is the open-subject input type and the open-result type. Open-path
// support is a planned feature; today's closed-polygon ops never produce one.
type Polyline []Point

// Result is the output of [Builder.Execute]. Closed holds the closed-polygon
// output (the same MultiPolygon the free functions return). Open holds any
// surviving open-subject chains; it is nil unless open subjects were added, so
// closed-only callers can ignore it.
type Result struct {
	Closed MultiPolygon
	Open   []Polyline
}

// Builder accumulates subject and clip polygons, then runs a boolean op over
// them with [Builder.Execute]. It is the general entry point that the named
// free functions ([Union], [Intersect], [Difference], [Xor]) wrap.
//
// A Builder is reusable: Add* accumulate inputs, Execute is non-destructive
// (run several ops over the same inputs), and [Builder.Reset] clears the
// accumulated inputs for a fresh set. A Builder is single-goroutine, the same
// rule as a MultiPolygon.
type Builder struct {
	subj MultiPolygon
	clip MultiPolygon
}

// NewBuilder returns an empty Builder.
func NewBuilder() *Builder {
	return &Builder{}
}

// AddSubject adds closed subject polygons. Multiple calls (and multiple
// MultiPolygons per call) aggregate: the subject set is the union of every
// piece added. Returns the receiver for chaining.
func (b *Builder) AddSubject(m ...MultiPolygon) *Builder {
	for _, mp := range m {
		b.subj = append(b.subj, mp...)
	}
	return b
}

// AddClip adds closed clip polygons. Like [Builder.AddSubject], multiple calls
// aggregate into a single clip set. Returns the receiver for chaining.
func (b *Builder) AddClip(m ...MultiPolygon) *Builder {
	for _, mp := range m {
		b.clip = append(b.clip, mp...)
	}
	return b
}

// Reset clears the accumulated subjects and clips so the Builder can be reused
// for a fresh set of inputs. Returns the receiver for chaining.
func (b *Builder) Reset() *Builder {
	b.subj = nil
	b.clip = nil
	return b
}

// Execute runs op over the accumulated subjects and clips and returns the
// result. It does not mutate the accumulated inputs, so it may be called
// repeatedly with different ops. Result.Open is nil (open paths are a planned
// feature).
func (b *Builder) Execute(op Operation) (Result, error) {
	closed, err := execOp(b.subj, b.clip, op)
	if err != nil {
		return Result{}, err
	}
	return Result{Closed: closed}, nil
}

// execOp is the single home for the per-op short-circuits, Xor-by-composition,
// and per-piece Difference decomposition. s is the aggregated subject set, c
// the aggregated clip set. The named free functions and Execute both route
// through here, so all callers get identical handling. The sweep path
// (runBooleanOp) carries the subset-invariant filter.
func execOp(s, c MultiPolygon, op Operation) (MultiPolygon, error) {
	switch op {
	case OpUnion:
		switch {
		case len(s) == 0 && len(c) == 0:
			return MultiPolygon{}, nil
		case len(s) == 0:
			return c, nil
		case len(c) == 0:
			return s, nil
		}
		// Idempotency: Union(A, A) = A. Identical inputs are a degenerate case
		// where every edge becomes a diff-src coincident pair at the SAME
		// vertex; the bound model's local-min disambiguation isn't designed for
		// that. Non-identical diff-src coincident cases are resolved by the
		// sweep's winding classification (DESIGN.md §12.6.1).
		if mpolyEqual(s, c) {
			return s, nil
		}
		if !s.BoundingBox().Intersects(c.BoundingBox()) {
			out := make(MultiPolygon, 0, len(s)+len(c))
			out = append(out, s...)
			out = append(out, c...)
			return out, nil
		}
		return runBooleanOp(s, c, clip.OpUnion)

	case OpIntersect:
		if len(s) == 0 || len(c) == 0 {
			return MultiPolygon{}, nil
		}
		if mpolyEqual(s, c) { // Intersect(A, A) = A
			return s, nil
		}
		if !s.BoundingBox().Intersects(c.BoundingBox()) {
			return MultiPolygon{}, nil
		}
		return runBooleanOp(s, c, clip.OpIntersect)

	case OpDifference:
		if len(s) == 0 {
			return MultiPolygon{}, nil
		}
		if len(c) == 0 {
			return s, nil
		}
		if mpolyEqual(s, c) { // Difference(A, A) = ∅
			return MultiPolygon{}, nil
		}
		if !s.BoundingBox().Intersects(c.BoundingBox()) {
			return s, nil
		}
		// Multipiece subject: (∪ᵢ Pᵢ) ∖ B = ∪ᵢ (Pᵢ ∖ B). A valid MultiPolygon's
		// pieces are disjoint, so differencing each independently is exact set
		// algebra and the results stay disjoint (plain concatenation). This
		// keeps every piece on the proven single-subject sweep path; a single
		// sweep over a multipiece subject over-traces at a coincident
		// cross-source vertical confluence (DESIGN.md §7.7). Per-piece, that
		// spurious lobe is a stray hole-free piece the subset filter drops.
		if len(s) > 1 {
			var out MultiPolygon
			for _, piece := range s {
				d, err := execOp(MultiPolygon{piece}, c, OpDifference)
				if err != nil {
					return nil, err
				}
				out = append(out, d...)
			}
			return out, nil
		}
		return runBooleanOp(s, c, clip.OpDifference)

	case OpXor:
		switch {
		case len(s) == 0 && len(c) == 0:
			return MultiPolygon{}, nil
		case len(s) == 0:
			return c, nil
		case len(c) == 0:
			return s, nil
		}
		if mpolyEqual(s, c) { // Xor(A, A) = ∅
			return MultiPolygon{}, nil
		}
		if !s.BoundingBox().Intersects(c.BoundingBox()) {
			out := make(MultiPolygon, 0, len(s)+len(c))
			out = append(out, s...)
			out = append(out, c...)
			return out, nil
		}
		// Xor = (A∪B) ∖ (A∩B), computed by composition rather than the direct
		// OpXor sweep, which mis-resolves a residual class of coincident /
		// cross-source confluences (DESIGN.md §7.6) that Union, Intersect and
		// Difference now handle correctly (incl. the subset-invariant filter).
		u, err := execOp(s, c, OpUnion)
		if err != nil {
			return nil, err
		}
		i, err := execOp(s, c, OpIntersect)
		if err != nil {
			return nil, err
		}
		return execOp(u, i, OpDifference)

	default:
		return nil, errUnknownOperation
	}
}
