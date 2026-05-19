package clip

import (
	"container/heap"

	"github.com/lestrrat-go/polyclip/fixed"
)

// EventKind tags the type of scanline event.
type EventKind uint8

const (
	// EventTop closes a segment: its top endpoint is reached and the
	// segment should be removed from the active edge list.
	EventTop EventKind = iota
	// EventBot opens a segment: its bottom endpoint is reached and the
	// segment should be inserted into the active edge list.
	EventBot
	// EventIntersection records that two segments cross at this point;
	// their relative order in the active edge list must be swapped.
	EventIntersection
)

// Event is a single scanline event. The sweep processes events in
// non-decreasing (P.Y, P.X) order; for ties at the same point, [EventKind]
// breaks the tie via its declared constant order (Top < Bot < Intersection).
//
// SegA always points at the segment most directly responsible for the event:
//
//   - For EventBot/EventTop, SegA is that segment.
//   - For EventIntersection, SegA and SegB are the two crossing segments.
type Event struct {
	Kind EventKind
	P    fixed.Point
	SegA *Segment
	SegB *Segment // only for EventIntersection
}

// Less reports whether e should be processed before f.
func (e Event) Less(f Event) bool {
	if e.P.Y != f.P.Y {
		return e.P.Y < f.P.Y
	}
	if e.P.X != f.P.X {
		return e.P.X < f.P.X
	}
	return e.Kind < f.Kind
}

// EventQueue is a min-heap of scanline [Event]s.
type EventQueue struct {
	heap eventHeap
}

// NewEventQueue returns an empty queue.
func NewEventQueue() *EventQueue {
	return &EventQueue{}
}

// Push enqueues an event.
func (q *EventQueue) Push(e Event) {
	heap.Push(&q.heap, e)
}

// Pop removes and returns the next event in (Y, X, Kind) order. Calling Pop
// on an empty queue panics; check [EventQueue.Len] first.
func (q *EventQueue) Pop() Event {
	e, ok := heap.Pop(&q.heap).(Event)
	if !ok {
		panic("clip: event queue corrupted: non-Event in heap")
	}
	return e
}

// Peek returns the next event without removing it. The zero [Event] is
// returned if the queue is empty.
func (q *EventQueue) Peek() Event {
	if len(q.heap) == 0 {
		return Event{}
	}
	return q.heap[0]
}

// Len returns the number of events in the queue.
func (q *EventQueue) Len() int { return len(q.heap) }

type eventHeap []Event

func (h eventHeap) Len() int           { return len(h) }
func (h eventHeap) Less(i, j int) bool { return h[i].Less(h[j]) }
func (h eventHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *eventHeap) Push(x any) {
	e, ok := x.(Event)
	if !ok {
		panic("clip: pushed non-Event onto eventHeap")
	}
	*h = append(*h, e)
}

func (h *eventHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}
