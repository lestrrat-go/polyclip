package polyclip

import (
	"math"
	"math/rand"
	"testing"

	"github.com/lestrrat-go/polyclip/geom"
	"github.com/stretchr/testify/require"
)

func rectAsPolygon(r geom.BBox) geom.MultiPolygon {
	return geom.New().
		Point(r.Min.X, r.Min.Y).
		Point(r.Max.X, r.Min.Y).
		Point(r.Max.X, r.Max.Y).
		Point(r.Min.X, r.Max.Y).
		MustBuild()
}

func mpArea(m geom.MultiPolygon) float64 {
	var a float64
	for i := range m {
		a += m[i].Area()
	}
	return a
}

func TestRectClip(t *testing.T) {
	cases := []struct {
		name string
		rect geom.BBox
		sub  geom.MultiPolygon
		// wantPieces, when checkPieces is true, asserts the exact number of
		// clipped pieces (require.Len). EmptyRect/FullyOutside expect 0.
		checkPieces bool
		wantPieces  int
		// wantArea, when checkArea is true, asserts the total clipped area
		// within 1e-9. Cases that do not set checkArea make no area claim.
		checkArea bool
		wantArea  float64
		// extra runs any additional bespoke assertions on the result.
		extra func(t *testing.T, got geom.MultiPolygon)
	}{
		{
			name:        "FullyInside",
			rect:        geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			sub:         geom.MultiPolygon{{Outer: geom.Polygon{{X: 2, Y: 2}, {X: 6, Y: 2}, {X: 6, Y: 6}, {X: 2, Y: 6}}}},
			checkPieces: true,
			wantPieces:  1,
			checkArea:   true,
			wantArea:    16,
			extra: func(t *testing.T, got geom.MultiPolygon) {
				require.True(t, got[0].Outer.IsCCW(), "outer ring not normalized to CCW")
			},
		},
		{
			name:        "FullyOutside",
			rect:        geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 5, Y: 5}},
			sub:         geom.MultiPolygon{{Outer: geom.Polygon{{X: 20, Y: 20}, {X: 30, Y: 20}, {X: 30, Y: 30}, {X: 20, Y: 30}}}},
			checkPieces: true,
			wantPieces:  0,
		},
		{
			// A small rect entirely within a large solid polygon clips to the full rect.
			name:      "RectInsideSolid",
			rect:      geom.BBox{Min: geom.Point{X: 40, Y: 40}, Max: geom.Point{X: 60, Y: 60}},
			sub:       geom.MultiPolygon{{Outer: geom.Polygon{{X: 0, Y: 0}, {X: 100, Y: 0}, {X: 100, Y: 100}, {X: 0, Y: 100}}}},
			checkArea: true,
			wantArea:  400,
		},
		{
			// Subject overlaps only the top-right corner region [6,10]x[6,10] of the rect.
			name:      "CornerOverlap",
			rect:      geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			sub:       geom.MultiPolygon{{Outer: geom.Polygon{{X: 6, Y: 6}, {X: 16, Y: 6}, {X: 16, Y: 16}, {X: 6, Y: 16}}}},
			checkArea: true,
			wantArea:  16,
		},
		{
			name:        "EmptyRect",
			rect:        geom.EmptyBBox(),
			sub:         geom.MultiPolygon{{Outer: geom.Polygon{{X: 0, Y: 0}, {X: 5, Y: 0}, {X: 5, Y: 5}, {X: 0, Y: 5}}}},
			checkPieces: true,
			wantPieces:  0,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RectClip(tc.sub, tc.rect)
			if tc.checkPieces {
				if tc.wantPieces == 0 {
					require.Empty(t, got, "got %d pieces, want 0", len(got))
				} else {
					require.Len(t, got, tc.wantPieces, "got %d pieces, want %d", len(got), tc.wantPieces)
				}
			}
			if tc.checkArea {
				require.InDelta(t, tc.wantArea, mpArea(got), 1e-9, "area = %v, want %v", mpArea(got), tc.wantArea)
			}
			if tc.extra != nil {
				tc.extra(t, got)
			}
		})
	}
}

func TestRectClipHolePreserved(t *testing.T) {
	rect := geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}}
	sub := geom.MultiPolygon{{
		Outer: geom.Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes: []geom.Polygon{{{X: 3, Y: 3}, {X: 3, Y: 7}, {X: 7, Y: 7}, {X: 7, Y: 3}}},
	}}
	got := RectClip(sub, rect)
	require.True(t, len(got) == 1 && len(got[0].Holes) == 1, "got %d pieces with holes %v, want 1 piece 1 hole", len(got), got)
	require.InDelta(t, 100-16, mpArea(got), 1e-9, "area want 84")
	require.False(t, got[0].Holes[0].IsCCW(), "hole not normalized to CW")
}

func TestRectClipConcaveSplitAreaParity(t *testing.T) {
	// A U-shape the rectangle splits into two prongs: Sutherland–Hodgman keeps
	// one seam-joined ring, but the enclosed area must still equal Intersect's.
	rect := geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 4}}
	u := geom.MultiPolygon{{Outer: geom.Polygon{
		{X: 1, Y: -2}, {X: 3, Y: -2}, {X: 3, Y: 8}, {X: 7, Y: 8},
		{X: 7, Y: -2}, {X: 9, Y: -2}, {X: 9, Y: 12}, {X: 1, Y: 12},
	}}}
	got := RectClip(u, rect)
	want, err := Intersect(u, rectAsPolygon(rect))
	require.NoError(t, err)
	require.InDelta(t, mpArea(want), mpArea(got), 1e-9, "area want %v (Intersect)", mpArea(want))
}

func TestRectClipIntersectParityRandom(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	randInt := func(lo, hi int) float64 { return float64(lo + rng.Intn(hi-lo+1)) }
	checked := 0
	for iter := range 2000 {
		// Random integer triangle (convex → SH output is a single piece, so the
		// area parity with Intersect is exact on the integer grid).
		tri := geom.Polygon{
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
			{X: randInt(-10, 20), Y: randInt(-10, 20)},
		}
		if tri.Area() < 1 {
			continue
		}
		x0, x1 := randInt(-5, 10), randInt(-5, 10)
		y0, y1 := randInt(-5, 10), randInt(-5, 10)
		if x0 == x1 || y0 == y1 {
			continue
		}
		rect := geom.BBox{
			Min: geom.Point{X: math.Min(x0, x1), Y: math.Min(y0, y1)},
			Max: geom.Point{X: math.Max(x0, x1), Y: math.Max(y0, y1)},
		}
		sub := geom.MultiPolygon{{Outer: tri}}
		got := RectClip(sub, rect)
		want, err := Intersect(sub, rectAsPolygon(rect))
		require.NoErrorf(t, err, "iter %d Intersect", iter)
		require.InDeltaf(t, mpArea(want), mpArea(got), 1e-6, "iter %d area, want %v; tri=%v rect=%v", iter, mpArea(want), tri, rect)
		checked++
	}
	require.GreaterOrEqual(t, checked, 1000, "only %d non-degenerate cases exercised, want > 1000", checked)
}

func TestRectClipLines(t *testing.T) {
	cases := []struct {
		name string
		rect geom.BBox
		in   []geom.Polyline
		// want is the expected clipped output; when empty is true the result
		// must be empty instead (require.Empty) and want is ignored.
		want  []geom.Polyline
		empty bool
	}{
		{
			name: "Crossing",
			rect: geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			in:   []geom.Polyline{{{X: -5, Y: 5}, {X: 15, Y: 5}}},
			want: []geom.Polyline{{{X: 0, Y: 5}, {X: 10, Y: 5}}},
		},
		{
			name: "FullyInside",
			rect: geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			in:   []geom.Polyline{{{X: 2, Y: 2}, {X: 5, Y: 8}, {X: 8, Y: 3}}},
			want: []geom.Polyline{{{X: 2, Y: 2}, {X: 5, Y: 8}, {X: 8, Y: 3}}},
		},
		{
			name:  "FullyOutside",
			rect:  geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			in:    []geom.Polyline{{{X: 20, Y: 20}, {X: 30, Y: 30}}},
			empty: true,
		},
		{
			// A path that dips out of the rect and back in is split into two polylines.
			name: "Reentry",
			rect: geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			in:   []geom.Polyline{{{X: 2, Y: 5}, {X: 2, Y: -5}, {X: 8, Y: -5}, {X: 8, Y: 5}}},
			want: []geom.Polyline{{{X: 2, Y: 5}, {X: 2, Y: 0}}, {{X: 8, Y: 0}, {X: 8, Y: 5}}},
		},
		{
			// A vertex sitting exactly on the boundary must not split the polyline.
			name: "TouchVertexStaysJoined",
			rect: geom.BBox{Min: geom.Point{X: 0, Y: 0}, Max: geom.Point{X: 10, Y: 10}},
			in:   []geom.Polyline{{{X: 2, Y: 2}, {X: 5, Y: 0}, {X: 8, Y: 2}}},
			want: []geom.Polyline{{{X: 2, Y: 2}, {X: 5, Y: 0}, {X: 8, Y: 2}}},
		},
		{
			name:  "EmptyRect",
			rect:  geom.EmptyBBox(),
			in:    []geom.Polyline{{{X: 0, Y: 0}, {X: 5, Y: 5}}},
			empty: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := RectClipLines(tc.in, tc.rect)
			if tc.empty {
				require.Empty(t, got, "got %v, want none", got)
				return
			}
			require.True(t, polylinesEqual(got, tc.want), "got %v, want %v", got, tc.want)
		})
	}
}

func polylinesEqual(a, b []geom.Polyline) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if len(a[i]) != len(b[i]) {
			return false
		}
		for j := range a[i] {
			if a[i][j] != b[i][j] {
				return false
			}
		}
	}
	return true
}
