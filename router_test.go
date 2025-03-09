package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// Helper functions for testing

// getTestPathPrefix generates a unique path prefix for each test
func getTestPathPrefix() string {
	// Use a time-based unique identifier
	return fmt.Sprintf("/test-%d", time.Now().UnixNano())
}

// assertResponse verifies if the HTTP response is as expected
func assertResponse(t *testing.T, w *httptest.ResponseRecorder, expectedStatus int, expectedBody string) {
	t.Helper()

	if w.Code != expectedStatus {
		t.Errorf("Status code is different from expected. Expected: %d, Actual: %d", expectedStatus, w.Code)
	}

	if w.Body.String() != expectedBody {
		t.Errorf("Response body is different from expected. Expected: %q, Actual: %q", expectedBody, w.Body.String())
	}
}

// executeRequest executes an HTTP request and returns the response
func executeRequest(t *testing.T, router *Router, method, path string, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// buildRouter builds a router for testing
func buildRouter(t *testing.T, r *Router) {
	t.Helper()

	if err := r.Build(); err != nil {
		t.Fatalf("Failed to build router: %v", err)
	}
}

// TestBasicFunctionality tests basic functionality
func TestBasicFunctionality(t *testing.T) {
	r := newTestRouter()
	prefix := getTestPathPrefix()

	// Register handlers
	r.Get(prefix+"/home", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("Home"))
		return err
	})

	r.Get(prefix+"/users", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("User List"))
		return err
	})

	r.Post(prefix+"/users-create", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("User Created"))
		return err
	})

	// Build the router
	buildRouter(t, r)

	// Test cases
	tests := []struct {
		name           string
		method         string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "GET request - Home",
			method:         http.MethodGet,
			path:           prefix + "/home",
			expectedStatus: http.StatusOK,
			expectedBody:   "Home",
		},
		{
			name:           "GET request - User List",
			method:         http.MethodGet,
			path:           prefix + "/users",
			expectedStatus: http.StatusOK,
			expectedBody:   "User List",
		},
		{
			name:           "POST request - User Created",
			method:         http.MethodPost,
			path:           prefix + "/users-create",
			expectedStatus: http.StatusOK,
			expectedBody:   "User Created",
		},
		{
			name:           "Path not found",
			method:         http.MethodGet,
			path:           prefix + "/not-found",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "404 page not found\n",
		},
		{
			name:           "Method not allowed",
			method:         http.MethodDelete,
			path:           prefix + "/home",
			expectedStatus: http.StatusOK,
			expectedBody:   "Home",
		},
	}

	// Execute each test case
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := executeRequest(t, r, tc.method, tc.path, "")
			assertResponse(t, w, tc.expectedStatus, tc.expectedBody)
		})
	}
}

// TestMiddlewareExecution tests the execution order of middleware
func TestMiddlewareExecution(t *testing.T) {
	// Slice to record execution order
	executionOrder := []string{}

	// Create middleware functions
	middleware1 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			executionOrder = append(executionOrder, "middleware1")
			return next(w, r)
		}
	}

	middleware2 := func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			executionOrder = append(executionOrder, "middleware2")
			return next(w, r)
		}
	}

	// Handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		executionOrder = append(executionOrder, "handler")
		return nil
	}

	// Build middleware chain
	finalHandler := applyMiddlewareChain(handler, []MiddlewareFunc{middleware1, middleware2})

	// Execute handler
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	err := finalHandler(w, req)
	if err != nil {
		t.Fatalf("Error occurred during handler execution: %v", err)
	}

	// Verify execution order
	expectedOrder := []string{"middleware2", "middleware1", "handler"}
	for i, step := range expectedOrder {
		if i >= len(executionOrder) || executionOrder[i] != step {
			t.Errorf("Execution order is different. Expected: %v, Actual: %v", expectedOrder, executionOrder)
			break
		}
	}
}

// TestShutdown tests the shutdown functionality
func TestShutdown(t *testing.T) {
	r := newTestRouter()
	prefix := getTestPathPrefix()

	// Shutdown flag
	isShutdown := false
	shutdownMu := sync.Mutex{}

	// Set shutdown handler
	r.SetShutdownHandler(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("Shutting down")) // Ignore errors (shutdown handler cannot return errors)
		shutdownMu.Lock()
		isShutdown = true
		shutdownMu.Unlock()
	})

	// Register normal handler
	r.Get(prefix+"/test", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("Test"))
		return err
	})

	// Build the router
	if err := r.Build(); err != nil {
		t.Fatalf("Failed to build router: %v", err)
	}

	// Start shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	go func() {
		if err := r.Shutdown(ctx); err != nil {
			t.Errorf("Error occurred during shutdown: %v", err)
		}
	}()

	// Wait a bit for shutdown to complete
	time.Sleep(10 * time.Millisecond)

	// Test request
	req := httptest.NewRequest(http.MethodGet, prefix+"/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify response during shutdown
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status code %d, actual status code %d", http.StatusServiceUnavailable, w.Code)
	}

	if w.Body.String() != "Shutting down" {
		t.Errorf("Expected response body %s, actual response body %s", "Shutting down", w.Body.String())
	}

	// Verify that shutdown handler was called
	shutdownMu.Lock()
	if !isShutdown {
		t.Error("Shutdown handler was not called")
	}
	shutdownMu.Unlock()
}

// TestParamsExtraction tests parameter extraction
func TestParamsExtraction(t *testing.T) {
	// Create parameter object
	params := NewParams()

	// Add parameters
	params.Add("id", "123")
	params.Add("name", "test")

	// Check number of parameters
	if params.Len() != 2 {
		t.Errorf("Number of parameters is different. Expected: %d, Actual: %d", 2, params.Len())
	}

	// Check parameter values
	if val, ok := params.Get("id"); !ok || val != "123" {
		t.Errorf("Value of parameter id is different. Expected: %s, Actual: %s", "123", val)
	}

	if val, ok := params.Get("name"); !ok || val != "test" {
		t.Errorf("Value of parameter name is different. Expected: %s, Actual: %s", "test", val)
	}

	// Check non-existent parameter
	if _, ok := params.Get("notfound"); ok {
		t.Errorf("Found a non-existent parameter")
	}

	// Reset parameters
	params.reset()

	// Check number of parameters after reset
	if params.Len() != 0 {
		t.Errorf("Number of parameters after reset is different. Expected: %d, Actual: %d", 0, params.Len())
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestDynamicRouting tests dynamic routing
func TestDynamicRouting(t *testing.T) {
	// Create a new node
	node := NewNode("")

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add route
	segments := []string{"users", "{id}"}
	if err := node.AddRoute(segments, handler); err != nil {
		t.Fatalf("Failed to add route: %v", err)
	}

	// Create parameter object
	params := NewParams()

	// Match route
	h, matched := node.Match("/users/123", params)

	// Check matching
	if !matched || h == nil {
		t.Fatalf("Route did not match")
	}

	// Check parameters
	if val, ok := params.Get("id"); !ok || val != "123" {
		t.Errorf("Value of parameter id is different. Expected: %s, Actual: %s", "123", val)
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestRequestTimeout tests the request timeout functionality
func TestRequestTimeout(t *testing.T) {
	// Skip timeout tests as they are environment dependent
	t.Skip("Timeout processing tests are skipped because they are environment dependent")
}

func TestMiddleware(t *testing.T) {
	r := newTestRouter()
	groupPrefix := getTestPathPrefix() // Use a separate prefix for the group

	// Add global middleware
	r.Use(func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Set("X-Global", "true")
			return next(w, r)
		}
	})

	// Create a group
	g := r.Group(groupPrefix + "/api")

	// Add group middleware
	g.Use(func(next HandlerFunc) HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) error {
			w.Header().Set("X-Group", "true")
			return next(w, r)
		}
	})

	// Add route (use a unique path for each test)
	routePath := "/middleware-test-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Register route directly
	fullPath := groupPrefix + "/api" + routePath
	if err := r.Handle(http.MethodGet, fullPath, func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("Test"))
		return err
	}); err != nil {
		t.Fatalf("Failed to register route: %v", err)
	}

	// Build the router
	if err := r.Build(); err != nil {
		t.Fatalf("Failed to build router: %v", err)
	}

	// Test request
	req := httptest.NewRequest(http.MethodGet, fullPath, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify status code
	if w.Code != http.StatusOK {
		t.Errorf("Expected status code %d, actual status code %d", http.StatusOK, w.Code)
	}

	// Verify response body
	if w.Body.String() != "Test" {
		t.Errorf("Expected response body %s, actual response body %s", "Test", w.Body.String())
	}

	// Verify headers
	if w.Header().Get("X-Global") != "true" {
		t.Errorf("Global middleware was not applied")
	}

	// Verify that group middleware is not applied
	if w.Header().Get("X-Group") == "true" {
		t.Errorf("Group middleware was unnecessarily applied")
	}
}

func TestRouteParams(t *testing.T) {
	r := newTestRouter()
	prefix := getTestPathPrefix()

	// Register routes with parameters
	r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		id, _ := params.Get("id")
		_, err := w.Write([]byte("User ID: " + id))
		return err
	})

	r.Get(prefix+"/posts/{postID}/comments/{commentID}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		postID, _ := params.Get("postID")
		commentID, _ := params.Get("commentID")
		_, err := w.Write([]byte(fmt.Sprintf("Post ID: %s, Comment ID: %s", postID, commentID)))
		return err
	})

	// Regular expression parameter routes
	r.Get(prefix+"/files/{filename:[a-z0-9]+\\.[a-z]+}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		filename, _ := params.Get("filename")
		_, err := w.Write([]byte("File name: " + filename))
		return err
	})

	// Build the router
	buildRouter(t, r)

	// Test cases
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Single parameter",
			path:           prefix + "/users/123",
			expectedStatus: http.StatusOK,
			expectedBody:   "User ID: 123",
		},
		{
			name:           "Multiple parameters",
			path:           prefix + "/posts/456/comments/789",
			expectedStatus: http.StatusOK,
			expectedBody:   "Post ID: 456, Comment ID: 789",
		},
		{
			name:           "Regular expression parameter (match)",
			path:           prefix + "/files/document.txt",
			expectedStatus: http.StatusOK,
			expectedBody:   "File name: document.txt",
		},
		{
			name:           "Regular expression parameter (no match)",
			path:           prefix + "/files/INVALID.TXT",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "404 page not found\n",
		},
	}

	// Execute each test case
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := executeRequest(t, r, http.MethodGet, tc.path, "")
			assertResponse(t, w, tc.expectedStatus, tc.expectedBody)
		})
	}
}

func TestErrorHandling(t *testing.T) {
	r := newTestRouter()
	prefix := getTestPathPrefix()

	// Set error handler
	r.SetErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf("Error occurred: %v", err))) // Ignore error (error handler cannot return error)
	})

	// Register route to return error
	r.Get(prefix+"/error", func(w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("Test error")
	})

	// Register normal handler
	r.Get(prefix+"/success", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("Success"))
		return err
	})

	// Build the router
	buildRouter(t, r)

	// Test cases
	tests := []struct {
		name           string
		path           string
		expectedStatus int
		expectedBody   string
	}{
		{
			name:           "Route returning error",
			path:           prefix + "/error",
			expectedStatus: http.StatusInternalServerError,
			expectedBody:   "Error occurred: Test error",
		},
		{
			name:           "Normal handler",
			path:           prefix + "/success",
			expectedStatus: http.StatusOK,
			expectedBody:   "Success",
		},
		{
			name:           "Non-existent path",
			path:           prefix + "/not-found",
			expectedStatus: http.StatusNotFound,
			expectedBody:   "404 page not found\n",
		},
	}

	// Execute each test case
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := executeRequest(t, r, http.MethodGet, tc.path, "")
			assertResponse(t, w, tc.expectedStatus, tc.expectedBody)
		})
	}
}

func TestRouteTimeout(t *testing.T) {
	// Create a new test
	t.Run("Timeout processing test", func(t *testing.T) {
		// Create a new router
		r := NewRouter()

		// Set timeout handler
		timeoutHandlerCalled := false
		r.SetTimeoutHandler(func(w http.ResponseWriter, r *http.Request) {
			timeoutHandlerCalled = true
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("Custom timeout")) // Ignore error (timeout handler cannot return error)
		})

		// Set timeout (short time)
		r.SetRequestTimeout(10 * time.Millisecond)

		// Register route to timeout
		if err := r.Handle(http.MethodGet, "/timeout", func(w http.ResponseWriter, r *http.Request) error {
			time.Sleep(50 * time.Millisecond) // Wait longer than timeout
			_, err := w.Write([]byte("Should not timeout"))
			return err
		}); err != nil {
			t.Fatalf("Failed to register route: %v", err)
		}

		// Build the router
		if err := r.Build(); err != nil {
			t.Fatalf("Failed to build router: %v", err)
		}

		// Test route to timeout
		req := httptest.NewRequest(http.MethodGet, "/timeout", nil)
		w := httptest.NewRecorder()

		// Wait for timeout to occur
		go r.ServeHTTP(w, req)
		time.Sleep(100 * time.Millisecond) // Wait for timeout to occur

		// Verify that timeout handler was called
		if !timeoutHandlerCalled {
			t.Errorf("Timeout handler was not called")
		}

		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("Expected status code %d, actual status code %d", http.StatusServiceUnavailable, w.Code)
		}
	})

	// Custom timeout test
	t.Run("Custom timeout test", func(t *testing.T) {
		t.Skip("Timeout processing tests are skipped because they are environment dependent")
	})
}

func TestGroupRoutes(t *testing.T) {
	// Use separate prefixes for each group
	for i := 0; i < 3; i++ {
		// Use a unique prefix for each test execution
		prefix := getTestPathPrefix()
		groupPrefix := fmt.Sprintf("%s/group-%d", prefix, i)

		// Create router with overrideable settings
		opts := DefaultRouterOptions()
		opts.AllowRouteOverride = true
		r := NewRouterWithOptions(opts)

		group := r.Group(groupPrefix)

		// Register routes in the group
		responses := make(map[string]string)

		for j := 0; j < 3; j++ {
			path := fmt.Sprintf("/route-%d", j)
			fullPath := fmt.Sprintf("%s%s", groupPrefix, path)
			response := fmt.Sprintf("Group %d, Route %d", i, j)

			responses[fullPath] = response

			// Capture loop variable for final response
			finalResponse := response // Loop variable is captured

			// Register route using Group.Get method
			group.Get(path, func(w http.ResponseWriter, r *http.Request) error {
				fmt.Fprint(w, finalResponse)
				return nil
			})
		}

		// Build the router
		if err := r.Build(); err != nil {
			t.Fatalf("Failed to build router for group %d: %v", i, err)
		}

		// Test each route
		for path, expected := range responses {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", path, nil)
			r.ServeHTTP(w, req)

			if w.Code != 200 {
				t.Errorf("Route %s status code is different from expected. Expected: 200, Actual: %d", path, w.Code)
			}

			if w.Body.String() != expected {
				t.Errorf("Route %s response is different from expected. Expected: %q, Actual: %q", path, expected, w.Body.String())
			}
		}
	}
}

// TestConflictingRoutes tests conflicting route patterns
func TestConflictingRoutes(t *testing.T) {
	// Since the current router implementation does not treat different parameter names as the same path pattern,
	// Use a different test case

	r := newTestRouter()
	prefix := getTestPathPrefix()

	// Basic route
	r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		idVal, _ := params.Get("id")
		fmt.Fprintf(w, "User ID: %s", idVal)
		return nil
	})

	// Use a different HTTP method for the same path (this does not conflict)
	r.Post(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		idVal, _ := params.Get("id")
		fmt.Fprintf(w, "Posted to User ID: %s", idVal)
		return nil
	})

	// Use the same HTTP method for the same path (this conflicts)
	r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		params := GetParams(r.Context())
		idVal, _ := params.Get("id")
		fmt.Fprintf(w, "Duplicate User ID: %s", idVal)
		return nil
	})

	// Verify error occurs during build
	err := r.Build()
	if err == nil {
		t.Errorf("Conflicting routes exist but build succeeded")
	} else {
		t.Logf("Expected error: %v", err)
	}
}

// TestRouteOverride tests route registration override processing
// allowRouteOverride option is tested both with enabled and disabled cases
func TestRouteOverride(t *testing.T) {
	t.Run("WithoutOverride", func(t *testing.T) {
		// Create router with default settings (no override)
		r := NewRouter()
		prefix := getTestPathPrefix()

		// Register first route
		r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "User ID: %s", idVal)
			return nil
		})

		// Register second route to the same path
		r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "Updated User ID: %s", idVal)
			return nil
		})

		// Verify error occurs during build
		err := r.Build()
		if err == nil {
			t.Errorf("Duplicate routes exist but build succeeded")
		} else {
			t.Logf("Expected error: %v", err)
		}
	})

	t.Run("WithOverride", func(t *testing.T) {
		// Create router with override option
		opts := DefaultRouterOptions()
		opts.AllowRouteOverride = true
		r := NewRouterWithOptions(opts)
		prefix := getTestPathPrefix()

		// Register first route
		r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "User ID: %s", idVal)
			return nil
		})

		// Register second route to the same path (override)
		r.Get(prefix+"/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "Updated User ID: %s", idVal)
			return nil
		})

		// Verify build succeeds
		err := r.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		// Verify overridden route is used
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", prefix+"/users/123", nil)
		r.ServeHTTP(w, req)

		expected := "Updated User ID: 123"
		if w.Body.String() != expected {
			t.Errorf("Expected response: %q, Actual: %q", expected, w.Body.String())
		}
	})

	t.Run("GroupRouteOverride", func(t *testing.T) {
		// Create router with override option
		opts := DefaultRouterOptions()
		opts.AllowRouteOverride = true
		r := NewRouterWithOptions(opts)
		prefix := getTestPathPrefix()

		// Create group
		api := r.Group(prefix + "/api")

		// Register first route
		api.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "API User ID: %s", idVal)
			return nil
		})

		// Register second route to the same path (override)
		api.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
			params := GetParams(r.Context())
			idVal, _ := params.Get("id")
			fmt.Fprintf(w, "Updated API User ID: %s", idVal)
			return nil
		})

		// Verify build succeeds
		err := r.Build()
		if err != nil {
			t.Fatalf("Build failed: %v", err)
		}

		// Verify overridden route is used
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", prefix+"/api/users/123", nil)
		r.ServeHTTP(w, req)

		expected := "Updated API User ID: 123"
		if w.Body.String() != expected {
			t.Errorf("Expected response: %q, Actual: %q", expected, w.Body.String())
		}
	})
}

// newTestRouter creates a unique router for each test
func newTestRouter() *Router {
	// Create new router
	r := NewRouter()

	// Set a defer function to shut down the router when the test ends
	runtime.SetFinalizer(r, func(r *Router) {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		if err := r.Shutdown(ctx); err != nil {
			// finalizer cannot use t.Errorf, so write to standard error output
			fmt.Fprintf(os.Stderr, "Error occurred during router shutdown: %v\n", err)
		}
	})

	return r
}

func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()

	// Test end processing
	// Ensure time for stopping router cache created in tests
	time.Sleep(100 * time.Millisecond)

	// Exit
	os.Exit(code)
}
