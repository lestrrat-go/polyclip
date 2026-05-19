package polyclip

import "fmt"

// Union returns a ∪ b.
//
// As of this version Union is implemented only for inputs whose bounding
// boxes are strictly disjoint — i.e. the two regions cannot share even a
// boundary point. For those inputs Union is equivalent to concatenation:
// every [ExPolygon] of a and every [ExPolygon] of b is preserved in the
// output exactly as given.
//
// For overlapping or boundary-touching inputs Union returns an error and a
// nil result. The full Vatti boolean engine that handles those cases is
// being built up incrementally; see DESIGN.md §11–§12 for the implementation
// plan.
//
// Empty inputs are accepted: Union(empty, b) returns b and Union(a, empty)
// returns a (in both cases sharing the input's slice — callers should treat
// the result as read-only or copy before mutating).
func Union(a, b MultiPolygon) (MultiPolygon, error) {
	switch {
	case len(a) == 0 && len(b) == 0:
		return MultiPolygon{}, nil
	case len(a) == 0:
		return b, nil
	case len(b) == 0:
		return a, nil
	}

	if a.BoundingBox().Intersects(b.BoundingBox()) {
		return nil, fmt.Errorf("polyclip: Union of overlapping or boundary-touching inputs is not yet implemented")
	}

	out := make(MultiPolygon, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	return out, nil
}
