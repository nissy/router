package router

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

// TestDoubleArrayTrieCreation tests the creation of a DoubleArrayTrie
func TestDoubleArrayTrieCreation(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Check initial state
	if trie.size != 1 {
		t.Errorf("Trie size is different. Expected: %d, Actual: %d", 1, trie.size)
	}

	if len(trie.base) < initialTrieSize {
		t.Errorf("Size of trie base array is too small. Expected: at least %d, Actual: %d", initialTrieSize, len(trie.base))
	}

	if len(trie.check) < initialTrieSize {
		t.Errorf("Size of trie check array is too small. Expected: at least %d, Actual: %d", initialTrieSize, len(trie.check))
	}

	if len(trie.handler) < initialTrieSize {
		t.Errorf("Size of trie handler array is too small. Expected: at least %d, Actual: %d", initialTrieSize, len(trie.handler))
	}

	if trie.base[rootNode] != baseOffset {
		t.Errorf("Base value of root node is different. Expected: %d, Actual: %d", baseOffset, trie.base[rootNode])
	}
}

// TestStaticRouteAdditionAndSearch tests adding and searching static routes
func TestStaticRouteAdditionAndSearch(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Test handler functions
	handler1 := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	handler2 := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add routes
	if err := trie.Add("/", handler1); err != nil {
		t.Fatalf("Failed to add root path: %v", err)
	}

	if err := trie.Add("/users", handler2); err != nil {
		t.Fatalf("Failed to add users path: %v", err)
	}

	// Search routes
	h1 := trie.Search("/")
	if h1 == nil {
		t.Fatalf("Root path not found")
	}

	h2 := trie.Search("/users")
	if h2 == nil {
		t.Fatalf("Users path not found")
	}

	// Search non-existent path
	h3 := trie.Search("/notfound")
	if h3 != nil {
		t.Fatalf("Non-existent path was found")
	}
}

// TestDuplicateRouteAddition tests adding duplicate routes
func TestDuplicateRouteAddition(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add first route
	if err := trie.Add("/users", handler); err != nil {
		t.Fatalf("Failed to add route: %v", err)
	}

	// Add the same route again
	err := trie.Add("/users", handler)
	if err == nil {
		t.Fatalf("Adding duplicate route succeeded")
	}

	// Check error type
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("Not the expected error type: %T", err)
	}

	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidPattern, routerErr.Code)
	}
}

// TestLongPathAddition tests adding a long path
func TestLongPathAddition(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add a long path
	longPath := "/users/profile/settings/notifications/email/daily"
	if err := trie.Add(longPath, handler); err != nil {
		t.Fatalf("Failed to add long path: %v", err)
	}

	// Search path
	h := trie.Search(longPath)
	if h == nil {
		t.Fatalf("Long path not found")
	}
}

// TestTrieExpansion tests array expansion of the trie
func TestTrieExpansion(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add multiple paths to test array expansion
	paths := []string{
		"/patho" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathx" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathy" + fmt.Sprintf("%d", time.Now().UnixNano()),
		"/pathz" + fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	for _, path := range paths {
		if err := trie.Add(path, handler); err != nil {
			t.Fatalf("Failed to add route: %v", err)
		}
	}

	// Search added paths
	for _, path := range paths {
		h := trie.Search(path)
		if h == nil {
			t.Errorf("Path %s not found", path)
		}
	}
}

// TestEmptyPathAddition tests adding an empty path
func TestEmptyPathAddition(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Test handler function
	handler := func(w http.ResponseWriter, r *http.Request) error {
		return nil
	}

	// Add empty path
	err := trie.Add("", handler)
	if err == nil {
		t.Fatalf("Adding empty path succeeded")
	}

	// Check error type
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("Not the expected error type: %T", err)
	}

	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidPattern, routerErr.Code)
	}
}

// TestNilHandlerAddition tests adding a nil handler
func TestNilHandlerAddition(t *testing.T) {
	// Create a new DoubleArrayTrie
	trie := newDoubleArrayTrie()

	// Add nil handler
	err := trie.Add("/test-nil-handler", nil)

	// Verify that an error occurs
	if err == nil {
		t.Fatalf("Adding nil handler succeeded")
	}

	// Check error type
	routerErr, ok := err.(*RouterError)
	if !ok {
		t.Fatalf("Not the expected error type: %T", err)
	}

	// Check error code
	if routerErr.Code != ErrInvalidPattern {
		t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidPattern, routerErr.Code)
	}

	// Check error message
	expectedMsg := "nil handler is not allowed"
	if routerErr.Message != expectedMsg {
		t.Errorf("Error message is different. Expected: %s, Actual: %s", expectedMsg, routerErr.Message)
	}
}
