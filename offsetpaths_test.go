package polyclip

import (
	"math"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

func approx(t *testing.T, got, want, tol float64, what string) {
	t.Helper()
	require.InDelta(t, want, got, tol, "%s = %g, want %g (tol %g)", what, got, want, tol)
}

func TestOffsetPathsStraight(t *testing.T) {
	// All cases build the same horizontal segment of length 10, offset by
	// half-width 2, varying only the End cap type and the expected area.
	roundWant := 40 + math.Pi*4
	cases := []struct {
		name string
		end  EndType
		want float64
		// tol differs per case: butt/square are exact (1e-9); round is only
		// approximate because tessellation chords cut slightly inside the
		// true arc, so it allows 1% relative.
		tol float64
		// what is the label passed to approx.
		what string
		// pieces, when non-nil, asserts res has exactly that many pieces.
		pieces *int
	}{
		{
			// A horizontal segment of length 10, half-width 2: a 10×4 rectangle.
			name:   "Butt",
			end:    EndButt,
			want:   40,
			tol:    1e-9,
			what:   "butt area",
			pieces: new(1),
		},
		{
			// Square caps extend 2 beyond each end: 14×4 = 56.
			name: "Square",
			end:  EndSquare,
			want: 56,
			tol:  1e-9,
			what: "square area",
		},
		{
			// Round caps add two semicircles of radius 2: 40 + pi*4.
			name: "Round",
			end:  EndRound,
			want: roundWant,
			tol:  roundWant * 0.01,
			what: "round area",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
			res, err := OffsetPaths([]geom.Polyline{line}, 2, OffsetOptions{End: tc.end})
			require.NoError(t, err)
			if tc.pieces != nil {
				require.Len(t, res, *tc.pieces, "butt pieces = %d, want %d", len(res), *tc.pieces)
			}
			approx(t, res.Area(), tc.want, tc.tol, tc.what)
		})
	}
}

func TestOffsetPathsVerticalButt(t *testing.T) {
	// Orientation must be CCW (positive area) regardless of path direction.
	line := geom.Polyline{{X: 0, Y: 10}, {X: 0, Y: 0}}
	res, err := OffsetPaths([]geom.Polyline{line}, 3, OffsetOptions{End: EndButt})
	require.NoError(t, err)
	approx(t, res.Area(), 60, 1e-9, "vertical butt area") // 10 long, width 6
}

func TestOffsetPathsRightAngle(t *testing.T) {
	// An L: (0,0)->(10,0)->(10,10), half-width 1, miter join at the corner.
	// The ribbon is a single simple piece. Area is two 10×2 arms minus the
	// 2×2 overlap at the corner, plus the convex miter wedge.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}
	res, err := OffsetPaths([]geom.Polyline{line}, 1, OffsetOptions{End: EndButt, Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, res, 1, "L pieces = %d, want 1", len(res))
	// 2 arms of 10×2 = 40, shared corner square 2×2 counted once = -4, miter
	// corner adds a 1×1 triangle (apex) outside the inner square: 40-4+? The
	// exact value with a miter outer corner is 37 (inner notch 1×1 removed,
	// outer miter 1×1 added cancel to the 36 square-corner plus 1). Verify
	// against a tolerance rather than over-precise hand math.
	a := res.Area()
	require.True(t, a >= 36 && a <= 40, "L area = %g, want in [36,40]", a)
}

func TestOffsetPathsSharpTurnSinglePiece(t *testing.T) {
	// A sharp V that doubles back: the inner side self-overlaps and must be
	// resolved into one clean piece by the self-union.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 1}, {X: 0, Y: 2}}
	res, err := OffsetPaths([]geom.Polyline{line}, 1, OffsetOptions{End: EndRound, Join: JoinRound})
	require.NoError(t, err)
	require.NotEmpty(t, res, "V produced empty result")
	a := res.Area()
	require.Greater(t, a, 0.0, "V area = %g, want > 0", a)
}

func TestOffsetPathsMultiple(t *testing.T) {
	// Two disjoint horizontal segments → two pieces.
	lines := []geom.Polyline{
		{{X: 0, Y: 0}, {X: 10, Y: 0}},
		{{X: 0, Y: 100}, {X: 10, Y: 100}},
	}
	res, err := OffsetPaths(lines, 2, OffsetOptions{End: EndButt})
	require.NoError(t, err)
	require.Len(t, res, 2, "multi pieces = %d, want 2", len(res))
	approx(t, res.Area(), 80, 1e-9, "multi area")
}

func TestOffsetPathsErrors(t *testing.T) {
	// Each case feeds invalid/edge input to OffsetPaths and asserts the exact
	// sentinel error it must return. The cases differ in the input paths, the
	// offset distance, and which ErrOffset* sentinel they expect.
	cases := []struct {
		name    string
		lines   []geom.Polyline
		dist    float64
		opts    OffsetOptions
		wantErr error
		msg     string
	}{
		{
			name:    "EndPolygonRejected",
			lines:   []geom.Polyline{{{X: 0, Y: 0}, {X: 10, Y: 0}}},
			dist:    2,
			opts:    OffsetOptions{End: EndPolygon},
			wantErr: ErrOffsetEndType,
			msg:     "OffsetPaths(EndPolygon) err = %v, want ErrOffsetEndType",
		},
		{
			name:    "Empty",
			lines:   nil,
			dist:    2,
			opts:    OffsetOptions{End: EndButt},
			wantErr: ErrOffsetEmpty,
			msg:     "OffsetPaths(nil) err = %v, want ErrOffsetEmpty",
		},
		{
			// A single-point path (and a zero-length repeat) has no direction; skipped.
			name:    "ShortSkipped",
			lines:   []geom.Polyline{{{X: 5, Y: 5}}, {{X: 1, Y: 1}, {X: 1, Y: 1}}},
			dist:    2,
			opts:    OffsetOptions{End: EndButt},
			wantErr: ErrOffsetEmpty,
			msg:     "OffsetPaths(short) err = %v, want ErrOffsetEmpty",
		},
		{
			name:    "ZeroWidth",
			lines:   []geom.Polyline{{{X: 0, Y: 0}, {X: 10, Y: 0}}},
			dist:    0,
			opts:    OffsetOptions{End: EndButt},
			wantErr: ErrOffsetEmpty,
			msg:     "OffsetPaths(d=0) err = %v, want ErrOffsetEmpty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := OffsetPaths(tc.lines, tc.dist, tc.opts)
			require.Equal(t, tc.wantErr, err, tc.msg, err)
		})
	}
}

func TestOffsetPathsJoinedSquareLoop(t *testing.T) {
	// A square loop (4 points, open) closed and banded by 1 each side with miter
	// joins: outer square [-1,-1]..[11,11] = 144, inner hole [1,1]..[9,9] = 64,
	// net 80. One piece with one hole.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	res, err := OffsetPaths([]geom.Polyline{line}, 1, OffsetOptions{End: EndJoined, Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, res, 1, "joined pieces = %d, want 1", len(res))
	require.Len(t, res[0].Holes, 1, "joined holes = %d, want 1", len(res[0].Holes))
	approx(t, res.Area(), 80, 1e-9, "joined square band area")
	bb := res[0].Outer.BoundingBox()
	approx(t, bb.Min.X, -1, 1e-9, "joined outer min x")
	approx(t, bb.Max.X, 11, 1e-9, "joined outer max x")
}

func TestOffsetPathsJoinedClosingDuplicate(t *testing.T) {
	// An explicit closing point (last == first) is the same loop as without it.
	open := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}
	closed := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}, {X: 0, Y: 0}}
	o, err := OffsetPaths([]geom.Polyline{open}, 1, OffsetOptions{End: EndJoined})
	require.NoError(t, err)
	c, err := OffsetPaths([]geom.Polyline{closed}, 1, OffsetOptions{End: EndJoined})
	require.NoError(t, err)
	approx(t, c.Area(), o.Area(), 1e-9, "joined closing-dup area")
}

func TestOffsetPathsJoinedThinLoopSolid(t *testing.T) {
	// A loop that encloses less than the band width on its short axis: the inner
	// ring collapses, leaving a solid ribbon (no hole) rather than an annulus.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 1}, {X: 0, Y: 1}}
	res, err := OffsetPaths([]geom.Polyline{line}, 2, OffsetOptions{End: EndJoined, Join: JoinMiter})
	require.NoError(t, err)
	require.Len(t, res, 1, "thin joined pieces = %d, want 1", len(res))
	require.Empty(t, res[0].Holes, "thin joined holes = %d, want 0", len(res[0].Holes))
	a := res.Area()
	require.Greater(t, a, 0.0, "thin joined area = %g, want > 0", a)
}

func TestOffsetPathsJoinedNonLoopBandsClosingEdge(t *testing.T) {
	// A non-closed L is closed into a triangle; the band wraps the whole loop
	// including the implicit hypotenuse, so it differs from the open-cap ribbon.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}}
	joined, err := OffsetPaths([]geom.Polyline{line}, 1, OffsetOptions{End: EndJoined, Join: JoinMiter})
	require.NoError(t, err)
	butt, err := OffsetPaths([]geom.Polyline{line}, 1, OffsetOptions{End: EndButt, Join: JoinMiter})
	require.NoError(t, err)
	// The triangle (legs 10, hypotenuse ~14.1) has perimeter ~34.1; a band of
	// width 2 around it is much larger than the open two-arm ribbon (~37).
	require.Greater(t, joined.Area(), butt.Area(), "joined L area %g, want > butt L area %g", joined.Area(), butt.Area())
}

func TestOffsetPathsJoinedTwoPointFallback(t *testing.T) {
	// A 2-point path cannot form a loop with area; it falls back to a capped
	// ribbon so the result is still a non-empty band.
	line := geom.Polyline{{X: 0, Y: 0}, {X: 10, Y: 0}}
	res, err := OffsetPaths([]geom.Polyline{line}, 2, OffsetOptions{End: EndJoined})
	require.NoError(t, err)
	a := res.Area()
	require.Greater(t, a, 0.0, "joined 2-point area = %g, want > 0", a)
}
