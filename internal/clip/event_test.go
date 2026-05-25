package clip

import (
	"testing"

	"github.com/lestrrat-go/polyclip/internal/fixed"
	"github.com/stretchr/testify/require"
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
	require.Equal(t, len(pushOrder), q.Len(), "Len: got %d want %d", q.Len(), len(pushOrder))

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
		require.True(t, got.P == w.P && got.Kind == w.Kind, "Pop[%d]: got %+v want %+v", i, got, w)
	}
	require.Equal(t, 0, q.Len(), "Len after drain: %d", q.Len())
}

func TestEventQueuePeek(t *testing.T) {
	q := NewEventQueue()
	require.Equal(t, Event{}, q.Peek(), "empty Peek: %+v", q.Peek())
	q.Push(ev(10, 0, EventBot))
	q.Push(ev(2, 0, EventBot))
	got := q.Peek()
	require.Equal(t, fixed.Coord(2), got.P.Y, "Peek: %+v want Y=2", got)
	// Peek must not mutate.
	require.Equal(t, 2, q.Len(), "Peek mutated length: %d", q.Len())
}

func TestEventLessTotal(t *testing.T) {
	a := ev(1, 2, EventTop)
	b := ev(1, 2, EventBot)
	c := ev(1, 2, EventIntersection)
	require.True(t, a.Less(b) && b.Less(c) && a.Less(c), "EventKind ordering broken")
	require.False(t, b.Less(a) || c.Less(b) || c.Less(a), "EventKind ordering not antisymmetric")
}
