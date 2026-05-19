package polyclip

import (
	"math"
	"testing"
)

// ===== §6.2 hand-built adversarial cases =====

func TestDifferenceAnnulus(t *testing.T) {
	// Outer 10x10 square minus inner 4x4 square produces an annulus —
	// outer ring with a hole. The inner square sits strictly inside the
	// outer with no edge touches; coincident edges are not exercised.
	outer := MultiPolygon{sq(0, 0, 10)}
	inner := MultiPolygon{sq(0, 0, 4)}
	got, err := Difference(outer, inner)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 piece, got %d: %+v", len(got), got)
	}
	if len(got[0].Holes) != 1 {
		t.Errorf("expected 1 hole, got %d", len(got[0].Holes))
	}
	wantArea := outer.Area() - inner.Area()
	if math.Abs(got.Area()-wantArea) > 0.01 {
		t.Errorf("Difference area %v want %v", got.Area(), wantArea)
	}
}

func TestIntersectAreaInvariantOverlappingDiamonds(t *testing.T) {
	// Area(Union) + Area(Intersect) == Area(A) + Area(B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	u, err := Union(a, b)
	if err != nil {
		t.Fatalf("Union err: %v", err)
	}
	i, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	lhs := u.Area() + i.Area()
	rhs := a.Area() + b.Area()
	if math.Abs(lhs-rhs) > 0.5 {
		t.Errorf("Area(Union)=%v + Area(Intersect)=%v = %v; Area(A)+Area(B)=%v", u.Area(), i.Area(), lhs, rhs)
	}
}

func TestDifferenceOverlappingDiamonds(t *testing.T) {
	// A ∖ B for two overlapping diamonds. Result area = Area(A) − Area(A∩B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Difference(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty difference")
	}
	inter, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	want := a.Area() - inter.Area()
	if math.Abs(got.Area()-want) > 0.5 {
		t.Errorf("Difference area %v want %v (=Area(A)−Area(A∩B))", got.Area(), want)
	}
}

func TestXorOverlappingDiamonds(t *testing.T) {
	// Xor(A,B) = symmetric difference. Area = Area(A) + Area(B) − 2·Area(A∩B).
	a := MultiPolygon{diamond(0, 0, 10)}
	b := MultiPolygon{diamond(5, 0, 10)}
	got, err := Xor(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got) == 0 {
		t.Fatalf("expected non-empty xor")
	}
	inter, err := Intersect(a, b)
	if err != nil {
		t.Fatalf("Intersect err: %v", err)
	}
	want := a.Area() + b.Area() - 2*inter.Area()
	if math.Abs(got.Area()-want) > 0.5 {
		t.Errorf("Xor area %v want %v", got.Area(), want)
	}
}

func TestUnionTouchingAtVertex(t *testing.T) {
	// Two diamonds touching at a single vertex (corner-to-corner). With
	// the source-based disambiguation in BuildLocalMinima, the two rings
	// are traced independently; the merged result is two ExPolygons (the
	// touch doesn't fuse the rings into one).
	a := MultiPolygon{diamond(0, 0, 5)}
	b := MultiPolygon{diamond(10, 0, 5)} // touches a at (5,0)
	got, err := Union(a, b)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Total area must equal sum of the two (touch is measure-zero overlap).
	wantArea := a.Area() + b.Area()
	if math.Abs(got.Area()-wantArea) > 0.5 {
		t.Errorf("Union area %v want %v; got=%+v", got.Area(), wantArea, got)
	}
}

// ===== §6.2 property invariants =====

func TestUnionIdempotent(t *testing.T) {
	// Union(A, A) should equal A (modulo orientation/start-vertex).
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Union(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(got.Area()-a.Area()) > 0.5 {
		t.Errorf("Union(A,A) area %v want %v", got.Area(), a.Area())
	}
}

func TestIntersectIdempotent(t *testing.T) {
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Intersect(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if math.Abs(got.Area()-a.Area()) > 0.5 {
		t.Errorf("Intersect(A,A) area %v want %v", got.Area(), a.Area())
	}
}

func TestDifferenceSelf(t *testing.T) {
	// Difference(A, A) should be empty.
	a := MultiPolygon{diamond(0, 0, 10)}
	got, err := Difference(a, a)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Area() > 0.5 {
		t.Errorf("Diff(A,A) area %v want ≈0", got.Area())
	}
}
