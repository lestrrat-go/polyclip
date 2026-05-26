package geom

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNoIssues(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}}, // CCW
		Holes: []Polygon{
			{{X: 2, Y: 2}, {X: 2, Y: 4}, {X: 4, Y: 4}, {X: 4, Y: 2}}, // CW
		},
	}}
	require.Nil(t, m.Validate(), "expected no issues, got %v", m.Validate())
}

func TestValidateTooFewVertices(t *testing.T) {
	m := MultiPolygon{ExPolygon{Outer: Polygon{{X: 0, Y: 0}, {X: 1, Y: 1}}}} // only 2 vertices
	issues := m.Validate()
	require.True(t, len(issues) == 1 && issues[0].Kind == IssueTooFewVertices, "expected one too-few-vertices issue, got %v", issues)
}

func TestValidateWrongWindingOuter(t *testing.T) {
	// CW outer (signed area < 0)
	m := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 0, Y: 10}, {X: 10, Y: 10}, {X: 10, Y: 0},
	}}}
	issues := m.Validate()
	require.True(t, len(issues) == 1 && issues[0].Kind == IssueWrongWinding, "expected wrong-winding issue, got %v", issues)
}

func TestValidateWrongWindingHole(t *testing.T) {
	// CCW hole (should be CW)
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes: []Polygon{{{X: 4, Y: 4}, {X: 6, Y: 4}, {X: 6, Y: 6}, {X: 4, Y: 6}}}, // CCW
	}}
	issues := m.Validate()
	require.True(t, len(issues) == 1 && issues[0].Kind == IssueWrongWinding && issues[0].Ring == 0, "expected wrong-winding hole issue, got %v", issues)
}

func TestValidateSelfIntersecting(t *testing.T) {
	// Bow-tie outer: edges (0,0)-(10,10) crosses (10,0)-(0,10).
	m := MultiPolygon{ExPolygon{Outer: Polygon{
		{X: 0, Y: 0}, {X: 10, Y: 10}, {X: 10, Y: 0}, {X: 0, Y: 10},
	}}}
	issues := m.Validate()
	found := false
	for _, iss := range issues {
		if iss.Kind == IssueSelfIntersecting {
			found = true
			break
		}
	}
	require.True(t, found, "expected self-intersecting issue, got %v", issues)
}

func TestValidateHoleOutsideOuter(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 10, Y: 0}, {X: 10, Y: 10}, {X: 0, Y: 10}},
		Holes: []Polygon{{{X: 20, Y: 20}, {X: 25, Y: 20}, {X: 25, Y: 25}, {X: 20, Y: 25}}}, // CCW, outside
	}}
	issues := m.Validate()
	// Expect both wrong-winding (CCW hole) and hole-outside-outer.
	foundOutside := false
	for _, iss := range issues {
		if iss.Kind == IssueHoleOutsideOuter {
			foundOutside = true
		}
	}
	require.True(t, foundOutside, "expected hole-outside-outer, got %v", issues)
}

func TestValidateOverlappingHoles(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{X: 0, Y: 0}, {X: 20, Y: 0}, {X: 20, Y: 20}, {X: 0, Y: 20}},
		Holes: []Polygon{
			{{X: 4, Y: 4}, {X: 4, Y: 12}, {X: 12, Y: 12}, {X: 12, Y: 4}}, // CW
			{{X: 8, Y: 8}, {X: 8, Y: 16}, {X: 16, Y: 16}, {X: 16, Y: 8}}, // CW, overlaps hole 0
		},
	}}
	issues := m.Validate()
	found := false
	for _, iss := range issues {
		if iss.Kind == IssueHolesOverlap {
			found = true
			break
		}
	}
	require.True(t, found, "expected holes-overlap issue, got %v", issues)
}

func TestValidateIssueString(t *testing.T) {
	iss := ValidationIssue{Kind: IssueWrongWinding, ExIdx: 1, Ring: 2, Msg: "test"}
	s := iss.String()
	require.True(t, strings.Contains(s, "wrong-winding") && strings.Contains(s, "ex[1]") && strings.Contains(s, "hole[2]"), "unexpected format: %s", s)
	iss.Ring = -1
	require.Contains(t, iss.String(), "outer", "expected 'outer' for ring=-1, got %s", iss.String())
}
