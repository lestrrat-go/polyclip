package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/fixed"
)

func ev(y, x int64, k EventKind) Event {
	return Event{Kind: k, P: fixed.Point{X: fixed.Coord(x), Y: fixed.Coord(y)}}
}

func TestEventQueueOrdering(t *testing.T) {
	q := NewEventQueue()
	// Push in scrambled order; expect ordered pops.
	pushOrder := []Event{
		ev(5, 0, EventBot),
		ev(3, 7, EventBot),
		ev(1, 0, EventBot),
		ev(1, -5, EventBot),
		ev(3, 7, EventTop), // same point as above but earlier Kind
		ev(3, 7, EventIntersection),
	}
	for _, e := range pushOrder {
		q.Push(e)
	}
	if q.Len() != len(pushOrder) {
		t.Fatalf("Len: got %d want %d", q.Len(), len(pushOrder))
	}

	want := []Event{
		ev(1, -5, EventBot),
		ev(1, 0, EventBot),
		ev(3, 7, EventTop),
		ev(3, 7, EventBot),
		ev(3, 7, EventIntersection),
		ev(5, 0, EventBot),
	}
	for i, w := range want {
		got := q.Pop()
		if got.P != w.P || got.Kind != w.Kind {
			t.Errorf("Pop[%d]: got %+v want %+v", i, got, w)
		}
	}
	if q.Len() != 0 {
		t.Errorf("Len after drain: %d", q.Len())
	}
}

func TestEventQueuePeek(t *testing.T) {
	q := NewEventQueue()
	if peek := q.Peek(); peek != (Event{}) {
		t.Errorf("empty Peek: %+v", peek)
	}
	q.Push(ev(10, 0, EventBot))
	q.Push(ev(2, 0, EventBot))
	got := q.Peek()
	if got.P.Y != 2 {
		t.Errorf("Peek: %+v want Y=2", got)
	}
	// Peek must not mutate.
	if q.Len() != 2 {
		t.Errorf("Peek mutated length: %d", q.Len())
	}
}

func TestEventLessTotal(t *testing.T) {
	a := ev(1, 2, EventTop)
	b := ev(1, 2, EventBot)
	c := ev(1, 2, EventIntersection)
	if !a.Less(b) || !b.Less(c) || !a.Less(c) {
		t.Error("EventKind ordering broken")
	}
	if b.Less(a) || c.Less(b) || c.Less(a) {
		t.Error("EventKind ordering not antisymmetric")
	}
}
