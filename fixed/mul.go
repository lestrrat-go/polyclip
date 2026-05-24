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

// abs returns the magnitude of x as an unsigned 128-bit value (hi, lo).
func (x I128) abs() (hi, lo uint64) {
	if x.Hi >= 0 {
		return uint64(x.Hi), x.Lo
	}
	lo, borrow := bits.Sub64(0, x.Lo, 0)
	hi = uint64(0) - uint64(x.Hi) - borrow
	return hi, lo
}

// u192 is an unsigned 192-bit integer, w0 the least-significant word.
type u192 struct{ w0, w1, w2 uint64 }

// mulU128U64 multiplies an unsigned 128-bit value (hi, lo) by m, exactly.
func mulU128U64(hi, lo, m uint64) u192 {
	p0hi, p0lo := bits.Mul64(lo, m)
	p1hi, p1lo := bits.Mul64(hi, m)
	w1, c := bits.Add64(p0hi, p1lo, 0)
	return u192{w0: p0lo, w1: w1, w2: p1hi + c}
}

// cmp reports -1, 0, +1 for a < b, a == b, a > b.
func (a u192) cmp(b u192) int {
	switch {
	case a.w2 != b.w2:
		if a.w2 < b.w2 {
			return -1
		}
		return 1
	case a.w1 != b.w1:
		if a.w1 < b.w1 {
			return -1
		}
		return 1
	case a.w0 != b.w0:
		if a.w0 < b.w0 {
			return -1
		}
		return 1
	}
	return 0
}

// CmpRationals returns the sign of na/da − nb/db, with da and db strictly
// positive, computed exactly: sign(na·db − nb·da) in full 192-bit precision.
// na, nb may be any [I128]; |na|·db and |nb|·da each fit in 192 bits for grid
// coordinates up to [MaxCoordMagnitude]. Used to order two edges by their X at
// a scanline without the rounding error of a float intercept.
func CmpRationals(na I128, da int64, nb I128, db int64) int {
	s1, s2 := na.Sign(), nb.Sign()
	if s1 != s2 {
		if s1 < s2 {
			return -1
		}
		return 1
	}
	if s1 == 0 {
		return 0
	}
	nahi, nalo := na.abs()
	nbhi, nblo := nb.abs()
	c := mulU128U64(nahi, nalo, uint64(db)).cmp(mulU128U64(nbhi, nblo, uint64(da)))
	if s1 > 0 {
		return c
	}
	return -c
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
