package geom

import (
	"errors"
	"fmt"
)

// Builder constructs a [MultiPolygon] fluently. The current piece's outer ring
// grows with [Builder.Point]; [Builder.Hole] attaches pre-built [Polygon] holes
// to that piece; [Builder.NextPiece] starts a new disjoint piece.
// [Builder.Build] (or [Builder.MustBuild]) normalizes winding — outer
// counter-clockwise, holes clockwise — and returns the result. A single piece
// needs no NextPiece call; the first piece is implicit.
//
// To build a hole's ring fluently, use a second Builder and [Builder.Polygon]:
// geom.New().Point(…).…​.Polygon() yields a [Polygon] to pass to Hole.
//
// Build only fixes winding; it does not resolve self-intersection. The engine
// assumes simple rings, so pass self-intersecting input through Simplify (and
// see [MultiPolygon.Validate] to detect it first).
//
// A Builder accumulates the first error rather than failing mid-chain, so the
// fluent chain never has to check between calls; the error surfaces at Build.
// A Builder is single-goroutine.
type Builder struct {
	// pieces always holds at least one element; the last is the piece that
	// Point and Hole currently target.
	pieces []ExPolygon
	err    error
}

// New returns an empty shape [Builder].
func New() *Builder {
	return &Builder{pieces: make([]ExPolygon, 1)}
}

// cur returns the piece currently under construction.
func (b *Builder) cur() *ExPolygon {
	return &b.pieces[len(b.pieces)-1]
}

// Point appends the vertex (x, y) to the current piece's outer ring. Returns
// the receiver for chaining.
func (b *Builder) Point(x, y float64) *Builder {
	if b.err == nil {
		c := b.cur()
		c.Outer = append(c.Outer, Point{X: x, Y: y})
	}
	return b
}

// Point3 appends the vertex (x, y) carrying the auxiliary Z coordinate (see
// [Point]) to the current piece's outer ring. Returns the receiver for
// chaining.
func (b *Builder) Point3(x, y, z float64) *Builder {
	if b.err == nil {
		c := b.cur()
		c.Outer = append(c.Outer, Point{X: x, Y: y, Z: z})
	}
	return b
}

// NextPiece starts a new disjoint piece; subsequent [Builder.Point] and
// [Builder.Hole] calls target it. It is a no-op when the current piece has no
// outer points yet, so a leading NextPiece (including geom.New().NextPiece())
// does nothing and a per-iteration NextPiece in a loop needs no first-iteration
// guard. Returns the receiver for chaining.
func (b *Builder) NextPiece() *Builder {
	if b.err != nil {
		return b
	}
	if len(b.cur().Outer) == 0 {
		return b
	}
	b.pieces = append(b.pieces, ExPolygon{})
	return b
}

// Hole attaches each given ring as a hole of the current piece. Rings may be
// pre-built [Polygon] values, composite literals, or — via [Builder.Polygon] —
// rings built fluently with another Builder. Because the parameter is the
// concrete [Polygon] type, a []Polygon spreads in directly: b.Hole(holes...).
// Each ring is copied, so the caller's slices are never retained or mutated.
// Winding need not be canonical; Build normalizes holes to clockwise. Returns
// the receiver for chaining.
func (b *Builder) Hole(rings ...Polygon) *Builder {
	if b.err == nil {
		c := b.cur()
		for _, r := range rings {
			c.Holes = append(c.Holes, append(Polygon(nil), r...))
		}
	}
	return b
}

// Build assembles the pieces into a [MultiPolygon] with winding normalized
// (outer CCW, holes CW). Empty pieces — those with no outer points, such as the
// one left by a trailing [Builder.NextPiece] — are dropped, so an unused
// Builder yields an empty MultiPolygon and no error. It returns an error if a
// non-empty piece's outer ring or any hole has fewer than three points, if a
// piece has holes but no outer ring, or if a bad hole was supplied. Build
// copies every ring, so it is non-destructive and may be called repeatedly.
func (b *Builder) Build() (MultiPolygon, error) {
	if b.err != nil {
		return nil, b.err
	}
	out := make(MultiPolygon, 0, len(b.pieces))
	for i := range b.pieces {
		src := &b.pieces[i]
		if len(src.Outer) == 0 {
			if len(src.Holes) > 0 {
				return nil, fmt.Errorf("geom: piece %d has holes but no outer ring", i)
			}
			continue
		}
		if len(src.Outer) < 3 {
			return nil, fmt.Errorf("geom: piece %d outer ring has %d points, need at least 3", i, len(src.Outer))
		}
		outer := append(Polygon(nil), src.Outer...)
		if !outer.IsCCW() {
			outer.Reverse()
		}
		ex := ExPolygon{Outer: outer}
		for hi, h := range src.Holes {
			if len(h) < 3 {
				return nil, fmt.Errorf("geom: piece %d hole %d has %d points, need at least 3", i, hi, len(h))
			}
			hole := append(Polygon(nil), h...)
			if hole.IsCCW() {
				hole.Reverse()
			}
			ex.Holes = append(ex.Holes, hole)
		}
		out = append(out, ex)
	}
	return out, nil
}

// MustBuild is [Builder.Build] but panics on error. It is intended for static,
// literal shape definitions — tests and fixtures — where a build failure is a
// programming mistake, not a runtime condition.
func (b *Builder) MustBuild() MultiPolygon {
	m, err := b.Build()
	if err != nil {
		panic(err)
	}
	return m
}

// Polygon returns the builder's single ring as a copied [Polygon], for building
// a ring fluently and using it elsewhere — most often as an argument to
// [Builder.Hole]. It errors if the builder holds more than one piece, if that
// piece carries holes, if the ring has fewer than three points, or if the
// builder already holds an error. The ring is returned as entered — winding is
// not normalized — so it can serve as either an outer ring or a hole once
// consumed.
func (b *Builder) Polygon() (Polygon, error) {
	if b.err != nil {
		return nil, b.err
	}
	if len(b.pieces) != 1 {
		return nil, fmt.Errorf("geom: Polygon requires a single piece, have %d", len(b.pieces))
	}
	p := b.pieces[0]
	if len(p.Holes) > 0 {
		return nil, errors.New("geom: Polygon requires a ring with no holes")
	}
	if len(p.Outer) < 3 {
		return nil, fmt.Errorf("geom: ring has %d points, need at least 3", len(p.Outer))
	}
	return append(Polygon(nil), p.Outer...), nil
}

// MustPolygon is [Builder.Polygon] but panics on error.
func (b *Builder) MustPolygon() Polygon {
	p, err := b.Polygon()
	if err != nil {
		panic(err)
	}
	return p
}
