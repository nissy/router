package router

import (
	"net/http"
	"testing"
)

// TestNodeCreation はノードの作成をテストします
func TestNodeCreation(t *testing.T) {
	node := NewNode("/users")
	if node == nil {
		t.Fatal("NewNode returned nil")
	}
	if node.segment != "/users" {
		t.Errorf("Expected pattern to be '/users', got '%s'", node.segment)
	}
	if node.handler != nil {
		t.Error("Expected handler to be nil")
	}
	if len(node.children) != 0 {
		t.Errorf("Expected children to be empty, got %d children", len(node.children))
	}
	if node.segmentType != staticSegment {
		t.Errorf("Expected segType to be %d (static), got %d", staticSegment, node.segmentType)
	}
}

// TestStaticRouteAddition は静的ルートの追加をテストします
func TestStaticRouteAddition(t *testing.T) {
	root := NewNode("")
	handler := func(w http.ResponseWriter, r *http.Request) error { return nil }

	err := root.AddRoute([]string{"users"}, handler)
	if err != nil {
		t.Fatalf("Failed to add route: %v", err)
	}

	if len(root.children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(root.children))
	}

	child := root.children[0]
	if child.segment != "users" {
		t.Errorf("Expected segment to be 'users', got '%s'", child.segment)
	}
	if child.segmentType != staticSegment {
		t.Errorf("Expected segmentType to be %d (static), got %d", staticSegment, child.segmentType)
	}
	if child.handler == nil {
		t.Error("Handler not set correctly")
	}
}

// TestParameterRouteAddition はパラメータルートの追加をテストします
func TestParameterRouteAddition(t *testing.T) {
	root := NewNode("")
	handler := func(w http.ResponseWriter, r *http.Request) error { return nil }

	err := root.AddRoute([]string{"users", "{id}"}, handler)
	if err != nil {
		t.Fatalf("Failed to add route: %v", err)
	}

	if len(root.children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(root.children))
	}

	child := root.children[0]
	if child.segment != "users" {
		t.Errorf("Expected segment to be 'users', got '%s'", child.segment)
	}

	if len(child.children) != 1 {
		t.Fatalf("Expected 1 grandchild, got %d", len(child.children))
	}

	grandchild := child.children[0]
	if grandchild.segment != "{id}" {
		t.Errorf("Expected segment to be '{id}', got '%s'", grandchild.segment)
	}
	if grandchild.segmentType != paramSegment {
		t.Errorf("Expected segmentType to be %d (param), got %d", paramSegment, grandchild.segmentType)
	}
	if grandchild.handler == nil {
		t.Error("Handler not set correctly")
	}
}

// TestRegexRouteAddition は正規表現ルートの追加をテストします
func TestRegexRouteAddition(t *testing.T) {
	root := NewNode("")
	handler := func(w http.ResponseWriter, r *http.Request) error { return nil }

	err := root.AddRoute([]string{"users", "{id:[0-9]+}"}, handler)
	if err != nil {
		t.Fatalf("Failed to add route: %v", err)
	}

	if len(root.children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(root.children))
	}

	child := root.children[0]
	if child.segment != "users" {
		t.Errorf("Expected segment to be 'users', got '%s'", child.segment)
	}

	if len(child.children) != 1 {
		t.Fatalf("Expected 1 grandchild, got %d", len(child.children))
	}

	grandchild := child.children[0]
	if grandchild.segment != "{id:[0-9]+}" {
		t.Errorf("Expected segment to be '{id:[0-9]+}', got '%s'", grandchild.segment)
	}
	if grandchild.segmentType != regexSegment {
		t.Errorf("Expected segmentType to be %d (regex), got %d", regexSegment, grandchild.segmentType)
	}
	if grandchild.regex == nil {
		t.Error("Regex not compiled")
	}
	if grandchild.handler == nil {
		t.Error("Handler not set correctly")
	}
}

// TestMultipleRoutes は複数のルートの追加と優先順位をテストします
func TestMultipleRoutes(t *testing.T) {
	root := NewNode("")
	handler := func(w http.ResponseWriter, r *http.Request) error { return nil }

	// Add multiple routes
	routes := [][]string{
		{"users", "list"},
		{"users", "{id}"},
		{"users", "{id}", "edit"},
		{"users", "{id:[0-9]+}", "profile"},
		{"admin", "dashboard"},
	}

	for _, route := range routes {
		if err := root.AddRoute(route, handler); err != nil {
			t.Fatalf("Failed to add route %v: %v", route, err)
		}
	}

	// Test matching
	testCases := []struct {
		path    string
		matches bool
		params  map[string]string
	}{
		{"/users/list", true, nil},
		{"/users/123", true, map[string]string{"id": "123"}},
		{"/users/123/edit", true, map[string]string{"id": "123"}},
		{"/users/123/profile", true, map[string]string{"id": "123"}},
		{"/users/abc/profile", false, nil},
		{"/admin/dashboard", true, nil},
		{"/unknown", false, nil},
	}

	for _, tc := range testCases {
		params := NewParams()
		h, matched := root.Match(tc.path, params)

		if tc.matches {
			if !matched || h == nil {
				t.Errorf("Path %s should match but didn't", tc.path)
			}

			// Check parameters
			if tc.params != nil {
				for k, v := range tc.params {
					val, ok := params.Get(k)
					if !ok || val != v {
						t.Errorf("Parameter %s should be %s, got %s", k, v, val)
					}
				}
			}
		} else {
			if matched || h != nil {
				t.Errorf("Path %s shouldn't match but did", tc.path)
			}
		}

		// Return params to pool
		params.reset()
	}
}

// TestExtractParamName はパラメータ名の抽出をテストします
func TestExtractParamName(t *testing.T) {
	testCases := []struct {
		pattern  string
		expected string
	}{
		{"{id}", "id"},
		{"{name}", "name"},
		{"{id:[0-9]+}", "id"},
		{"{slug:[a-z-]+}", "slug"},
	}

	for _, tc := range testCases {
		result := extractParamName(tc.pattern)
		if result != tc.expected {
			t.Errorf("extractParamName(%s) = %s, expected %s", tc.pattern, result, tc.expected)
		}
	}
}
