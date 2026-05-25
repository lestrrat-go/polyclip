package polyclip

import (
	"math"
	"math/rand"
	"testing"
)

// polylinesEqual (rectclip_test.go) compares open-path result sets for exact
// point equality, order-sensitive (open output follows path order).

func openResult(t *testing.T, b *Builder, op Operation) []Polyline {
	t.Helper()
	res, err := b.Execute(op)
	if err != nil {
		t.Fatalf("Execute(%v): %v", op, err)
	}
	return res.Open
}

func pl(pts ...Point) Polyline { return Polyline(pts) }
func pt(x, y float64) Point    { return Point{X: x, Y: y} }

// TestOpenPathCrossingClip clips a horizontal line straddling a clip square and
// checks each op keeps the documented portions (Clipper2 IsContributingOpen).
func TestOpenPathCrossingClip(t *testing.T) {
	clip := mpRect(2, 2, 8, 8)
	line := pl(pt(-1, 5), pt(11, 5))

	inside := []Polyline{pl(pt(2, 5), pt(8, 5))}
	outside := []Polyline{pl(pt(-1, 5), pt(2, 5)), pl(pt(8, 5), pt(11, 5))}

	cases := []struct {
		op   Operation
		want []Polyline
	}{
		{OpIntersect, inside},
		{OpDifference, outside},
		{OpXor, outside},
		{OpUnion, outside}, // no closed subject, so Union keeps outside the clip
	}
	for _, c := range cases {
		b := NewBuilder().AddOpenSubject(line).AddClip(clip)
		got := openResult(t, b, c.op)
		if !polylinesEqual(got, c.want) {
			t.Errorf("op %v: got %v, want %v", c.op, got, c.want)
		}
	}
}

// TestOpenPathUnionWithSubject confirms Union open clipping also removes the
// portions inside a closed subject region (predicate !inSubj && !inClip).
func TestOpenPathUnionWithSubject(t *testing.T) {
	subj := mpRect(0, 4, 4, 6) // covers x in (0,4) at y=5
	clip := mpRect(2, 2, 8, 8) // covers x in (2,8) at y=5
	line := pl(pt(-1, 5), pt(11, 5))

	b := NewBuilder().AddOpenSubject(line).AddSubject(subj).AddClip(clip)
	got := openResult(t, b, OpUnion)
	// Inside subject∪clip is x in (0,8); keep outside: [-1,0] and [8,11].
	want := []Polyline{pl(pt(-1, 5), pt(0, 5)), pl(pt(8, 5), pt(11, 5))}
	if !polylinesEqual(got, want) {
		t.Errorf("Union open got %v, want %v", got, want)
	}
}

// TestOpenPathStitchAcrossVertex checks that a kept run continues across a path
// vertex lying inside the clip (the L-shaped line stays one polyline).
func TestOpenPathStitchAcrossVertex(t *testing.T) {
	clip := mpRect(2, 2, 8, 8)
	line := pl(pt(-1, 5), pt(5, 5), pt(5, -1))

	b := NewBuilder().AddOpenSubject(line).AddClip(clip)
	got := openResult(t, b, OpIntersect)
	want := []Polyline{pl(pt(2, 5), pt(5, 5), pt(5, 2))}
	if !polylinesEqual(got, want) {
		t.Errorf("stitch got %v, want %v", got, want)
	}
}

// TestOpenPathThroughVertex clips a diagonal that enters and exits a square
// exactly through its corners; the corner passes must register as crossings.
func TestOpenPathThroughVertex(t *testing.T) {
	clip := mpRect(2, 2, 8, 8)
	line := pl(pt(0, 0), pt(10, 10))

	b := NewBuilder().AddOpenSubject(line).AddClip(clip)
	got := openResult(t, b, OpIntersect)
	want := []Polyline{pl(pt(2, 2), pt(8, 8))}
	if !polylinesEqual(got, want) {
		t.Errorf("through-vertex got %v, want %v", got, want)
	}
}

// TestOpenPathMultipleClips clips a line crossing two disjoint clip squares;
// Intersect yields one chain per square.
func TestOpenPathMultipleClips(t *testing.T) {
	line := pl(pt(0, 5), pt(20, 5))
	b := NewBuilder().AddOpenSubject(line).
		AddClip(mpRect(2, 2, 6, 8)).
		AddClip(mpRect(12, 2, 16, 8))
	got := openResult(t, b, OpIntersect)
	want := []Polyline{pl(pt(2, 5), pt(6, 5)), pl(pt(12, 5), pt(16, 5))}
	if !polylinesEqual(got, want) {
		t.Errorf("multi-clip got %v, want %v", got, want)
	}
}

// TestOpenPathEmptyClip checks degenerate operands: Intersect with no clip
// drops everything; Difference keeps the whole line.
func TestOpenPathEmptyClip(t *testing.T) {
	line := pl(pt(0, 0), pt(10, 0))

	gotI := openResult(t, NewBuilder().AddOpenSubject(line), OpIntersect)
	if len(gotI) != 0 {
		t.Errorf("Intersect empty clip: got %v, want none", gotI)
	}
	gotD := openResult(t, NewBuilder().AddOpenSubject(line), OpDifference)
	want := []Polyline{line}
	if !polylinesEqual(gotD, want) {
		t.Errorf("Difference empty clip: got %v, want %v", gotD, want)
	}
}

// TestOpenPathFullyInside / outside need no splitting.
func TestOpenPathWholeRuns(t *testing.T) {
	clip := mpRect(0, 0, 10, 10)
	inside := pl(pt(2, 2), pt(8, 8))

	gotI := openResult(t, NewBuilder().AddOpenSubject(inside).AddClip(clip), OpIntersect)
	if !polylinesEqual(gotI, []Polyline{inside}) {
		t.Errorf("inside Intersect: got %v, want whole line", gotI)
	}
	gotD := openResult(t, NewBuilder().AddOpenSubject(inside).AddClip(clip), OpDifference)
	if len(gotD) != 0 {
		t.Errorf("inside Difference: got %v, want none", gotD)
	}
}

// TestOpenPathNoOpenSubject confirms closed-only Execute leaves Open nil.
func TestOpenPathNoOpenSubject(t *testing.T) {
	res, err := NewBuilder().AddSubject(mpRect(0, 0, 4, 4)).
		AddClip(mpRect(2, 2, 6, 6)).Execute(OpIntersect)
	if err != nil {
		t.Fatal(err)
	}
	if res.Open != nil {
		t.Errorf("closed-only Open = %v, want nil", res.Open)
	}
}

// TestOpenPathShortDropped checks polylines with fewer than two points produce
// no output and do not panic.
func TestOpenPathShortDropped(t *testing.T) {
	b := NewBuilder().
		AddOpenSubject(pl(pt(5, 5))). // single point
		AddOpenSubject(pl()).         // empty
		AddClip(mpRect(0, 0, 10, 10))
	got := openResult(t, b, OpIntersect)
	if len(got) != 0 {
		t.Errorf("short polylines: got %v, want none", got)
	}
}

// TestOpenPathReset confirms Reset clears accumulated open subjects.
func TestOpenPathReset(t *testing.T) {
	b := NewBuilder().AddOpenSubject(pl(pt(0, 0), pt(10, 0)))
	b.Reset()
	res, err := b.Execute(OpDifference)
	if err != nil {
		t.Fatal(err)
	}
	if res.Open != nil {
		t.Errorf("after Reset, Open = %v, want nil", res.Open)
	}
}

// distToSeg returns the Euclidean distance from p to segment ab.
func distToSeg(a, b, p Point) float64 {
	ab := b.Sub(a)
	l2 := ab.Dot(ab)
	if l2 == 0 {
		return math.Sqrt(p.Sub(a).Dot(p.Sub(a)))
	}
	t := p.Sub(a).Dot(ab) / l2
	if t < 0 {
		t = 0
	} else if t > 1 {
		t = 1
	}
	proj := Point{X: a.X + t*ab.X, Y: a.Y + t*ab.Y}
	return math.Sqrt(p.Sub(proj).Dot(p.Sub(proj)))
}

// onPolylines reports whether p lies (within margin) on any segment of lines.
func onPolylines(lines []Polyline, p Point, margin float64) bool {
	for _, ln := range lines {
		for i := 0; i+1 < len(ln); i++ {
			if distToSeg(ln[i], ln[i+1], p) <= margin {
				return true
			}
		}
	}
	return false
}

// nearAnyRing reports whether p is within margin of any boundary ring edge,
// where the keep predicate is ambiguous and split rounding may differ.
func nearAnyRing(rings [][]Point, p Point, margin float64) bool {
	for _, ring := range rings {
		n := len(ring)
		for i := range ring {
			if distToSeg(ring[i], ring[(i+1)%n], p) <= margin {
				return true
			}
		}
	}
	return false
}

// TestOpenPathSampledOracle validates splitting/stitching: every sampled point
// along an open subject that the keep predicate retains must lie on the clipped
// output, and every dropped point must not — checked away from boundaries where
// membership is ambiguous. Random clips, subjects, and polylines.
func TestOpenPathSampledOracle(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	const margin = 0.02
	ri := func(hi int) float64 { return float64(rng.Intn(hi)) }
	randRect := func() MultiPolygon {
		x0, y0 := ri(10), ri(10)
		return mpRect(x0, y0, x0+1+ri(6), y0+1+ri(6))
	}
	for iter := range 1500 {
		clip := randRect()
		if rng.Intn(2) == 0 {
			clip = append(clip, randRect()...)
		}
		subj := randRect()
		npts := 2 + rng.Intn(3)
		line := make(Polyline, npts)
		for i := range line {
			line[i] = pt(ri(14)-2, ri(14)-2)
		}
		for _, op := range []Operation{OpIntersect, OpDifference, OpXor, OpUnion} {
			keep, rings := openKeep(op, subj, clip)
			out := clipOpenPaths([]Polyline{line}, op, subj, clip)
			for i := 0; i+1 < len(line); i++ {
				a, b := line[i], line[i+1]
				if a == b {
					continue
				}
				for s := 1; s < 20; s++ {
					p := lerpPoint(a, b, float64(s)/20)
					if nearAnyRing(rings, p, margin) {
						continue
					}
					if keep(p) != onPolylines(out, p, margin) {
						t.Fatalf("iter %d op %v: point %v keep=%v on-output=%v\nline=%v clip=%v subj=%v\nout=%v",
							iter, op, p, keep(p), onPolylines(out, p, margin), line, clip, subj, out)
					}
				}
			}
		}
	}
}
