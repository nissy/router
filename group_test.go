package router

import (
	"net/http"
	"testing"
)

// TestGroupCreation tests the creation of a group
func TestGroupCreation(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Create a group
	g := r.Group("/api")

	// Check the initial state of the group
	if g == nil {
		t.Fatalf("Failed to create group")
	}

	if g.prefix != "/api" {
		t.Errorf("Group prefix is different. Expected: %s, Actual: %s", "/api", g.prefix)
	}

	if len(g.middleware) != 0 {
		t.Errorf("Group middleware is not initialized")
	}

	if len(g.routes) != 0 {
		t.Errorf("Group routes are not initialized")
	}

	if g.router != r {
		t.Errorf("Group router is not set correctly")
	}
}

// TestGroupWithMiddleware tests the creation of a group with middleware
func TestGroupWithMiddleware(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Test middleware functions
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// Create a group with middleware
	g := r.Group("/api", middleware1, middleware2)

	// Check the group's middleware
	if len(g.middleware) != 2 {
		t.Errorf("Number of group middleware is different. Expected: %d, Actual: %d", 2, len(g.middleware))
	}
}

// TestNestedGroups tests the creation of nested groups
func TestNestedGroups(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Test middleware functions
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// Create a parent group
	parent := r.Group("/api", middleware1)

	// Create a child group
	child := parent.Group("/v1", middleware2)

	// Check the child group's prefix
	if child.prefix != "/api/v1" {
		t.Errorf("Child group prefix is different. Expected: %s, Actual: %s", "/api/v1", child.prefix)
	}

	// Check the child group's middleware
	if len(child.middleware) != 2 {
		t.Errorf("Number of child group middleware is different. Expected: %d, Actual: %d", 2, len(child.middleware))
	}
}

// TestGroupUse tests adding middleware to a group
func TestGroupUse(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Create a group
	g := r.Group("/api")

	// Test middleware functions
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// Add middleware
	g.Use(middleware1)
	g.Use(middleware2)

	// Check the group's middleware
	if len(g.middleware) != 2 {
		t.Errorf("Number of group middleware is different. Expected: %d, Actual: %d", 2, len(g.middleware))
	}
}

// TestGroupRoute tests the Route method of a group
func TestGroupRoute(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Create a group
	g := r.Group("/api")

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Test middleware function
	middleware := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			return next(w, r)
		}
	}

	// Create a route
	route := g.Route(http.MethodGet, "/users", handler, middleware)

	// Check the route
	if route == nil {
		t.Fatalf("Failed to create route")
	}

	if route.method != http.MethodGet {
		t.Errorf("Route method is different. Expected: %s, Actual: %s", http.MethodGet, route.method)
	}

	if route.subPath != "/users" {
		t.Errorf("Route path is different. Expected: %s, Actual: %s", "/users", route.subPath)
	}

	if route.handler == nil {
		t.Errorf("Route handler is not set")
	}

	if len(route.middleware) != 1 {
		t.Errorf("Number of route middleware is different. Expected: %d, Actual: %d", 1, len(route.middleware))
	}

	if route.group != g {
		t.Errorf("Route group is not set correctly")
	}

	// Check the group's routes
	if len(g.routes) != 1 {
		t.Errorf("Number of group routes is different. Expected: %d, Actual: %d", 1, len(g.routes))
	}
}

// TestGroupHTTPMethods tests the HTTP methods of a group
func TestGroupHTTPMethods(t *testing.T) {
	// Create a new router
	r := NewRouter()

	// Create a group
	g := r.Group("/api")

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Create routes for each HTTP method
	getRoute := g.Get("/users", handler)
	postRoute := g.Post("/users", handler)
	putRoute := g.Put("/users/{id}", handler)
	deleteRoute := g.Delete("/users/{id}", handler)
	patchRoute := g.Patch("/users/{id}", handler)
	headRoute := g.Head("/users", handler)
	optionsRoute := g.Options("/users", handler)

	// Check each route
	if getRoute == nil || getRoute.method != http.MethodGet {
		t.Errorf("GET route is not created correctly")
	}

	if postRoute == nil || postRoute.method != http.MethodPost {
		t.Errorf("POST route is not created correctly")
	}

	if putRoute == nil || putRoute.method != http.MethodPut {
		t.Errorf("PUT route is not created correctly")
	}

	if deleteRoute == nil || deleteRoute.method != http.MethodDelete {
		t.Errorf("DELETE route is not created correctly")
	}

	if patchRoute == nil || patchRoute.method != http.MethodPatch {
		t.Errorf("PATCH route is not created correctly")
	}

	if headRoute == nil || headRoute.method != http.MethodHead {
		t.Errorf("HEAD route is not created correctly")
	}

	if optionsRoute == nil || optionsRoute.method != http.MethodOptions {
		t.Errorf("OPTIONS route is not created correctly")
	}

	// Check the number of group routes
	if len(g.routes) != 7 {
		t.Errorf("Number of group routes is different. Expected: %d, Actual: %d", 7, len(g.routes))
	}
}
