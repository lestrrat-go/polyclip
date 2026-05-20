package polyclip

import (
	"strings"
	"testing"
)

func TestValidateNoIssues(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}}, // CCW
		Holes: []Polygon{
			{{2, 2}, {2, 4}, {4, 4}, {4, 2}}, // CW
		},
	}}
	if got := m.Validate(); got != nil {
		t.Errorf("expected no issues, got %v", got)
	}
}

func TestValidateTooFewVertices(t *testing.T) {
	m := MultiPolygon{ExPolygon{Outer: Polygon{{0, 0}, {1, 1}}}} // only 2 vertices
	issues := m.Validate()
	if len(issues) != 1 || issues[0].Kind != IssueTooFewVertices {
		t.Errorf("expected one too-few-vertices issue, got %v", issues)
	}
}

func TestValidateWrongWindingOuter(t *testing.T) {
	// CW outer (signed area < 0)
	m := MultiPolygon{ExPolygon{Outer: Polygon{
		{0, 0}, {0, 10}, {10, 10}, {10, 0},
	}}}
	issues := m.Validate()
	if len(issues) != 1 || issues[0].Kind != IssueWrongWinding {
		t.Errorf("expected wrong-winding issue, got %v", issues)
	}
}

func TestValidateWrongWindingHole(t *testing.T) {
	// CCW hole (should be CW)
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}},
		Holes: []Polygon{{{4, 4}, {6, 4}, {6, 6}, {4, 6}}}, // CCW
	}}
	issues := m.Validate()
	if len(issues) != 1 || issues[0].Kind != IssueWrongWinding || issues[0].Ring != 0 {
		t.Errorf("expected wrong-winding hole issue, got %v", issues)
	}
}

func TestValidateSelfIntersecting(t *testing.T) {
	// Bow-tie outer: edges (0,0)-(10,10) crosses (10,0)-(0,10).
	m := MultiPolygon{ExPolygon{Outer: Polygon{
		{0, 0}, {10, 10}, {10, 0}, {0, 10},
	}}}
	issues := m.Validate()
	found := false
	for _, iss := range issues {
		if iss.Kind == IssueSelfIntersecting {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected self-intersecting issue, got %v", issues)
	}
}

func TestValidateHoleOutsideOuter(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{0, 0}, {10, 0}, {10, 10}, {0, 10}},
		Holes: []Polygon{{{20, 20}, {25, 20}, {25, 25}, {20, 25}}}, // CCW, outside
	}}
	issues := m.Validate()
	// Expect both wrong-winding (CCW hole) and hole-outside-outer.
	foundOutside := false
	for _, iss := range issues {
		if iss.Kind == IssueHoleOutsideOuter {
			foundOutside = true
		}
	}
	if !foundOutside {
		t.Errorf("expected hole-outside-outer, got %v", issues)
	}
}

func TestValidateOverlappingHoles(t *testing.T) {
	m := MultiPolygon{ExPolygon{
		Outer: Polygon{{0, 0}, {20, 0}, {20, 20}, {0, 20}},
		Holes: []Polygon{
			{{4, 4}, {4, 12}, {12, 12}, {12, 4}}, // CW
			{{8, 8}, {8, 16}, {16, 16}, {16, 8}}, // CW, overlaps hole 0
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
	if !found {
		t.Errorf("expected holes-overlap issue, got %v", issues)
	}
}

func TestValidateIssueString(t *testing.T) {
	iss := ValidationIssue{Kind: IssueWrongWinding, ExIdx: 1, Ring: 2, Msg: "test"}
	s := iss.String()
	if !strings.Contains(s, "wrong-winding") || !strings.Contains(s, "ex[1]") || !strings.Contains(s, "hole[2]") {
		t.Errorf("unexpected format: %s", s)
	}
	iss.Ring = -1
	if s := iss.String(); !strings.Contains(s, "outer") {
		t.Errorf("expected 'outer' for ring=-1, got %s", s)
	}
}
