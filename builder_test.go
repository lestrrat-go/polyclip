package polyclip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

// mpRect is a unit-test helper building a CCW axis-aligned rectangle MultiPolygon.
func mpRect(x0, y0, x1, y1 float64) geom.MultiPolygon {
	return geom.New().
		Point(x0, y0).Point(x1, y0).Point(x1, y1).Point(x0, y1).
		MustBuild()
}

// TestBuilderMatchesFreeFunctions asserts the accumulator's Execute is
// byte-identical to the named free functions across overlapping, disjoint,
// identical, empty and multipiece inputs — the step-0 behavior-preserving
// contract (DESIGN.md §7.8).
func TestBuilderMatchesFreeFunctions(t *testing.T) {
	overlapA := mpRect(0, 0, 4, 4)
	overlapB := mpRect(2, 2, 6, 6)
	disjointB := mpRect(10, 10, 12, 12)
	multiA := geom.MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	carveB := mpRect(1, -1, 3, 5)

	cases := []struct {
		name string
		a, b geom.MultiPolygon
	}{
		{"overlap", overlapA, overlapB},
		{"disjoint", overlapA, disjointB},
		{"identical", overlapA, overlapA},
		{"emptyClip", overlapA, geom.MultiPolygon{}},
		{"emptySubject", geom.MultiPolygon{}, overlapB},
		{"bothEmpty", geom.MultiPolygon{}, geom.MultiPolygon{}},
		{"multipieceDiff", multiA, carveB},
	}

	ops := []struct {
		op   Operation
		free func(a, b geom.MultiPolygon) (geom.MultiPolygon, error)
	}{
		{OpUnion, Union},
		{OpIntersect, Intersect},
		{OpDifference, Difference},
		{OpXor, Xor},
	}

	for _, tc := range cases {
		for _, o := range ops {
			want, werr := o.free(tc.a, tc.b)
			got, gerr := New().AddSubject(tc.a).AddClip(tc.b).Execute(o.op)
			require.Equal(t, werr == nil, gerr == nil, "%s op=%d: error mismatch free=%v clipper=%v", tc.name, o.op, werr, gerr)
			if werr != nil {
				continue
			}
			require.True(t, mpolyEqual(want, got.Closed), "%s op=%d: Execute=%v want free=%v", tc.name, o.op, got.Closed, want)
			require.Nil(t, got.Open, "%s op=%d: Open should be nil, got %v", tc.name, o.op, got.Open)
		}
	}
}

// TestBuilderAccumulatesAndResets checks that multiple Add* calls aggregate
// their pieces into a single subject/clip set, that Execute is non-destructive
// (repeatable), and that Reset clears the inputs.
func TestBuilderAccumulatesAndResets(t *testing.T) {
	c := New().
		AddSubject(mpRect(0, 0, 2, 2)).
		AddSubject(mpRect(0, 4, 2, 6)).
		AddClip(mpRect(1, -1, 3, 5))

	// Aggregated two subject pieces against one clip == one multipiece subject.
	wantSubj := geom.MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	want, err := Difference(wantSubj, mpRect(1, -1, 3, 5))
	require.NoError(t, err)

	first, err := c.Execute(OpDifference)
	require.NoError(t, err)
	require.True(t, mpolyEqual(want, first.Closed), "accumulated Difference=%v want %v", first.Closed, want)

	// Execute is non-destructive: a second call yields the same result.
	second, err := c.Execute(OpDifference)
	require.NoError(t, err)
	require.True(t, mpolyEqual(first.Closed, second.Closed), "second Execute=%v differs from first %v", second.Closed, first.Closed)

	// Reset clears inputs: Difference of nothing is empty.
	got, err := c.Reset().Execute(OpDifference)
	require.NoError(t, err)
	require.Empty(t, got.Closed, "after Reset, Execute=%v want empty", got.Closed)
}

// TestBuilderUnknownOperation asserts an out-of-range Operation errors rather
// than silently producing wrong output.
func TestBuilderUnknownOperation(t *testing.T) {
	_, err := New().AddSubject(mpRect(0, 0, 1, 1)).Execute(Operation(99))
	require.Error(t, err, "Execute with unknown op: want error, got nil")
}
