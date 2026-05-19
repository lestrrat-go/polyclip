package fixed

import "math"

// Coord is one component of a fixed-point integer coordinate.
type Coord int64

// Point is a fixed-point 2D point on the integer grid.
type Point struct {
	X, Y Coord
}

// MaxCoordMagnitude is the largest absolute coordinate value the engine
// targets. Coordinate differences therefore fit in [-2^61, 2^61], and the
// products that show up in segment-intersection determinants are bounded by
// 2^122 — within the 128-bit range used by [I128].
const MaxCoordMagnitude = int64(1) << 60

// Scale converts between float64 user coordinates and fixed-point integer
// coordinates by centering the user-space bounding box on (OffsetX, OffsetY)
// and multiplying by Factor.
//
// The mapping is:
//
//	internal = round((user - Offset) * Factor)
//	user     = float64(internal) / Factor + Offset
//
// Factor is a power of two so it can be applied without losing the exactness
// of inputs that already lie on a coarser grid. The same Factor is used for
// both axes so angles and orientations are preserved.
type Scale struct {
	Factor  float64
	OffsetX float64
	OffsetY float64
}

// ScaleFromBBox returns a [Scale] mapping the user-space rectangle
// [minX, maxX]×[minY, maxY] onto integer coordinates whose magnitudes are at
// most [MaxCoordMagnitude].
//
// The bounding box is centered on its midpoint and scaled uniformly so the
// longer axis just fills the target range. For a degenerate (zero-area)
// bounding box the function returns the largest power-of-two Factor that
// keeps subsequent operations safe; the underlying engine should reject such
// input before reaching this layer.
func ScaleFromBBox(minX, minY, maxX, maxY float64) Scale {
	cx := (minX + maxX) / 2
	cy := (minY + maxY) / 2
	halfSpan := math.Max(maxX-minX, maxY-minY) / 2
	target := float64(MaxCoordMagnitude)

	var factor float64
	if halfSpan <= 0 {
		// Degenerate input — pick the largest factor that still leaves
		// headroom. The exact value is not load-bearing because the engine
		// rejects empty/degenerate inputs upstream.
		factor = math.Exp2(math.Floor(math.Log2(target)))
	} else {
		factor = math.Exp2(math.Floor(math.Log2(target / halfSpan)))
	}
	return Scale{Factor: factor, OffsetX: cx, OffsetY: cy}
}

// Snap maps a float64 user point onto the integer grid.
func (s Scale) Snap(x, y float64) Point {
	return Point{
		X: Coord(math.Round((x - s.OffsetX) * s.Factor)),
		Y: Coord(math.Round((y - s.OffsetY) * s.Factor)),
	}
}

// Unsnap maps an integer-grid point back to float64 user coordinates.
func (s Scale) Unsnap(p Point) (x, y float64) {
	return float64(p.X)/s.Factor + s.OffsetX, float64(p.Y)/s.Factor + s.OffsetY
}
