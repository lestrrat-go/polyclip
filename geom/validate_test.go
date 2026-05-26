package geom

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNoIssues(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(), // CCW
		Holes: []Polygon{
			New().Point(2, 2).Point(2, 4).Point(4, 4).Point(4, 2).MustPolygon(), // CW
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
	m := MultiPolygon{ExPolygon{Outer: New().
		Point(0, 0).Point(0, 10).Point(10, 10).Point(10, 0).
		MustPolygon()}}
	issues := m.Validate()
	require.True(t, len(issues) == 1 && issues[0].Kind == IssueWrongWinding, "expected wrong-winding issue, got %v", issues)
}

func TestValidateWrongWindingHole(t *testing.T) {
	// CCW hole (should be CW)
	m := MultiPolygon{ExPolygon{
		Outer: New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(),
		Holes: []Polygon{New().Point(4, 4).Point(6, 4).Point(6, 6).Point(4, 6).MustPolygon()}, // CCW
	}}
	issues := m.Validate()
	require.True(t, len(issues) == 1 && issues[0].Kind == IssueWrongWinding && issues[0].Ring == 0, "expected wrong-winding hole issue, got %v", issues)
}

func TestValidateSelfIntersecting(t *testing.T) {
	// Bow-tie outer: edges (0,0)-(10,10) crosses (10,0)-(0,10).
	m := MultiPolygon{ExPolygon{Outer: New().
		Point(0, 0).Point(10, 10).Point(10, 0).Point(0, 10).
		MustPolygon()}}
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
		Outer: New().Point(0, 0).Point(10, 0).Point(10, 10).Point(0, 10).MustPolygon(),
		Holes: []Polygon{New().Point(20, 20).Point(25, 20).Point(25, 25).Point(20, 25).MustPolygon()}, // CCW, outside
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
		Outer: New().Point(0, 0).Point(20, 0).Point(20, 20).Point(0, 20).MustPolygon(),
		Holes: []Polygon{
			New().Point(4, 4).Point(4, 12).Point(12, 12).Point(12, 4).MustPolygon(), // CW
			New().Point(8, 8).Point(8, 16).Point(16, 16).Point(16, 8).MustPolygon(), // CW, overlaps hole 0
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
