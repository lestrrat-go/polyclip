package polyclip

import "testing"

// mpRect is a unit-test helper building a CCW axis-aligned rectangle MultiPolygon.
func mpRect(x0, y0, x1, y1 float64) MultiPolygon {
	return MultiPolygon{{Outer: Polygon{
		{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1},
	}}}
}

// TestBuilderMatchesFreeFunctions asserts the accumulator's Execute is
// byte-identical to the named free functions across overlapping, disjoint,
// identical, empty and multipiece inputs — the step-0 behavior-preserving
// contract (DESIGN.md §7.8).
func TestBuilderMatchesFreeFunctions(t *testing.T) {
	overlapA := mpRect(0, 0, 4, 4)
	overlapB := mpRect(2, 2, 6, 6)
	disjointB := mpRect(10, 10, 12, 12)
	multiA := MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	carveB := mpRect(1, -1, 3, 5)

	cases := []struct {
		name string
		a, b MultiPolygon
	}{
		{"overlap", overlapA, overlapB},
		{"disjoint", overlapA, disjointB},
		{"identical", overlapA, overlapA},
		{"emptyClip", overlapA, MultiPolygon{}},
		{"emptySubject", MultiPolygon{}, overlapB},
		{"bothEmpty", MultiPolygon{}, MultiPolygon{}},
		{"multipieceDiff", multiA, carveB},
	}

	ops := []struct {
		op   Operation
		free func(a, b MultiPolygon) (MultiPolygon, error)
	}{
		{OpUnion, Union},
		{OpIntersect, Intersect},
		{OpDifference, Difference},
		{OpXor, Xor},
	}

	for _, tc := range cases {
		for _, o := range ops {
			want, werr := o.free(tc.a, tc.b)
			got, gerr := NewBuilder().AddSubject(tc.a).AddClip(tc.b).Execute(o.op)
			if (werr == nil) != (gerr == nil) {
				t.Fatalf("%s op=%d: error mismatch free=%v clipper=%v", tc.name, o.op, werr, gerr)
			}
			if werr != nil {
				continue
			}
			if !mpolyEqual(want, got.Closed) {
				t.Errorf("%s op=%d: Execute=%v want free=%v", tc.name, o.op, got.Closed, want)
			}
			if got.Open != nil {
				t.Errorf("%s op=%d: Open should be nil, got %v", tc.name, o.op, got.Open)
			}
		}
	}
}

// TestBuilderAccumulatesAndResets checks that multiple Add* calls aggregate
// their pieces into a single subject/clip set, that Execute is non-destructive
// (repeatable), and that Reset clears the inputs.
func TestBuilderAccumulatesAndResets(t *testing.T) {
	c := NewBuilder().
		AddSubject(mpRect(0, 0, 2, 2)).
		AddSubject(mpRect(0, 4, 2, 6)).
		AddClip(mpRect(1, -1, 3, 5))

	// Aggregated two subject pieces against one clip == one multipiece subject.
	wantSubj := MultiPolygon{mpRect(0, 0, 2, 2)[0], mpRect(0, 4, 2, 6)[0]}
	want, err := Difference(wantSubj, mpRect(1, -1, 3, 5))
	if err != nil {
		t.Fatal(err)
	}

	first, err := c.Execute(OpDifference)
	if err != nil {
		t.Fatal(err)
	}
	if !mpolyEqual(want, first.Closed) {
		t.Errorf("accumulated Difference=%v want %v", first.Closed, want)
	}

	// Execute is non-destructive: a second call yields the same result.
	second, err := c.Execute(OpDifference)
	if err != nil {
		t.Fatal(err)
	}
	if !mpolyEqual(first.Closed, second.Closed) {
		t.Errorf("second Execute=%v differs from first %v", second.Closed, first.Closed)
	}

	// Reset clears inputs: Difference of nothing is empty.
	got, err := c.Reset().Execute(OpDifference)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Closed) != 0 {
		t.Errorf("after Reset, Execute=%v want empty", got.Closed)
	}
}

// TestBuilderUnknownOperation asserts an out-of-range Operation errors rather
// than silently producing wrong output.
func TestBuilderUnknownOperation(t *testing.T) {
	if _, err := NewBuilder().AddSubject(mpRect(0, 0, 1, 1)).Execute(Operation(99)); err == nil {
		t.Error("Execute with unknown op: want error, got nil")
	}
}
