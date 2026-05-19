package polyclip

import (
	"math"
	"testing"
)

func TestBBoxEmpty(t *testing.T) {
	e := EmptyBBox()
	if !e.Empty() {
		t.Fatalf("EmptyBBox() should be empty, got Min=%v Max=%v", e.Min, e.Max)
	}
	// Zero BBox represents the single point at the origin, not empty.
	var z BBox
	if z.Empty() {
		t.Fatalf("zero BBox should not be Empty()")
	}
}

func TestBBoxAdd(t *testing.T) {
	b := EmptyBBox()
	b = b.Add(Point{1, 2})
	b = b.Add(Point{-1, 5})
	b = b.Add(Point{4, 3})
	want := BBox{Min: Point{-1, 2}, Max: Point{4, 5}}
	if b != want {
		t.Fatalf("Add: got %+v want %+v", b, want)
	}
}

func TestBBoxUnion(t *testing.T) {
	a := BBox{Min: Point{0, 0}, Max: Point{10, 10}}
	b := BBox{Min: Point{5, -5}, Max: Point{20, 5}}
	got := a.Union(b)
	want := BBox{Min: Point{0, -5}, Max: Point{20, 10}}
	if got != want {
		t.Fatalf("Union: got %+v want %+v", got, want)
	}
	if got := a.Union(EmptyBBox()); got != a {
		t.Fatalf("Union with empty changed value: %+v", got)
	}
	if got := EmptyBBox().Union(a); got != a {
		t.Fatalf("empty.Union(a) != a: %+v", got)
	}
}

func TestBBoxContains(t *testing.T) {
	b := BBox{Min: Point{0, 0}, Max: Point{10, 10}}
	cases := []struct {
		p  Point
		in bool
	}{
		{Point{5, 5}, true},
		{Point{0, 0}, true},
		{Point{10, 10}, true},
		{Point{10, 5}, true},
		{Point{-0.001, 5}, false},
		{Point{5, 10.001}, false},
	}
	for _, c := range cases {
		if got := b.Contains(c.p); got != c.in {
			t.Errorf("Contains(%v): got %v want %v", c.p, got, c.in)
		}
	}
	if EmptyBBox().Contains(Point{0, 0}) {
		t.Errorf("empty bbox should contain no points")
	}
}

func TestBBoxWidthHeight(t *testing.T) {
	b := BBox{Min: Point{1, 2}, Max: Point{4, 7}}
	if b.Width() != 3 {
		t.Errorf("Width: %v want 3", b.Width())
	}
	if b.Height() != 5 {
		t.Errorf("Height: %v want 5", b.Height())
	}
	e := EmptyBBox()
	if e.Width() != 0 || e.Height() != 0 {
		t.Errorf("empty box w/h should be 0/0, got %v/%v", e.Width(), e.Height())
	}
}

// Sanity: math.Inf used in EmptyBBox should behave as expected with Empty().
func TestBBoxEmptyInfinity(t *testing.T) {
	e := EmptyBBox()
	if !math.IsInf(e.Min.X, +1) || !math.IsInf(e.Max.X, -1) {
		t.Fatalf("EmptyBBox infinities wrong: %+v", e)
	}
}
