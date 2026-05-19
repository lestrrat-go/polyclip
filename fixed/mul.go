package fixed

import "math/bits"

// I128 is a signed 128-bit integer represented in two's complement as a
// signed high half and an unsigned low half.
type I128 struct {
	Hi int64
	Lo uint64
}

// MulI64 returns a*b exactly as an [I128].
func MulI64(a, b int64) I128 {
	neg := false
	ua := uint64(a)
	if a < 0 {
		neg = !neg
		ua = -ua // wraps correctly for math.MinInt64
	}
	ub := uint64(b)
	if b < 0 {
		neg = !neg
		ub = -ub
	}
	hi, lo := bits.Mul64(ua, ub)
	if neg {
		nlo, carry := bits.Add64(^lo, 1, 0)
		nhi := ^hi + carry
		return I128{Hi: int64(nhi), Lo: nlo}
	}
	return I128{Hi: int64(hi), Lo: lo}
}

// Add returns x+y.
func (x I128) Add(y I128) I128 {
	lo, carry := bits.Add64(x.Lo, y.Lo, 0)
	hi := int64(uint64(x.Hi) + uint64(y.Hi) + carry)
	return I128{Hi: hi, Lo: lo}
}

// Sub returns x-y.
func (x I128) Sub(y I128) I128 {
	lo, borrow := bits.Sub64(x.Lo, y.Lo, 0)
	hi := int64(uint64(x.Hi) - uint64(y.Hi) - borrow)
	return I128{Hi: hi, Lo: lo}
}

// Sign returns -1 if x is negative, 0 if zero, +1 if positive.
func (x I128) Sign() int {
	if x.Hi < 0 {
		return -1
	}
	if x.Hi == 0 && x.Lo == 0 {
		return 0
	}
	return +1
}

// IsZero reports whether x is zero.
func (x I128) IsZero() bool {
	return x.Hi == 0 && x.Lo == 0
}

// Orient2D returns the sign of the determinant
//
//	(q.X - p.X) * (r.Y - p.Y) - (q.Y - p.Y) * (r.X - p.X)
//
// computed in full 128-bit precision. The return value distinguishes the
// orientation of the triangle (p, q, r):
//
//	+1 — counter-clockwise (left turn)
//	 0 — collinear
//	-1 — clockwise (right turn)
//
// Inputs are expected to be on the engine's integer grid with magnitudes at
// most [MaxCoordMagnitude]; coordinate differences then fit in int64.
func Orient2D(p, q, r Point) int {
	ax := int64(q.X) - int64(p.X)
	ay := int64(q.Y) - int64(p.Y)
	bx := int64(r.X) - int64(p.X)
	by := int64(r.Y) - int64(p.Y)
	return MulI64(ax, by).Sub(MulI64(ay, bx)).Sign()
}
