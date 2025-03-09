package router

import (
	"testing"
)

// TestRouterError tests the creation and string conversion of RouterError
func TestRouterError(t *testing.T) {
	// Create a new RouterError
	err := &RouterError{
		Code:    ErrInvalidPattern,
		Message: "Test error message",
	}

	// Check error code
	if err.Code != ErrInvalidPattern {
		t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidPattern, err.Code)
	}

	// Check error message
	if err.Message != "Test error message" {
		t.Errorf("Error message is different. Expected: %s, Actual: %s", "Test error message", err.Message)
	}

	// Check string representation of the error
	expected := "InvalidPattern: Test error message"
	if err.Error() != expected {
		t.Errorf("String representation of the error is different. Expected: %s, Actual: %s", expected, err.Error())
	}
}

// TestErrorCodes tests the definition of error codes
func TestErrorCodes(t *testing.T) {
	// Check error code definitions
	if ErrInvalidPattern != 1 {
		t.Errorf("Value of ErrInvalidPattern is different. Expected: %d, Actual: %d", 1, ErrInvalidPattern)
	}

	if ErrInvalidMethod != 2 {
		t.Errorf("Value of ErrInvalidMethod is different. Expected: %d, Actual: %d", 2, ErrInvalidMethod)
	}

	if ErrNilHandler != 3 {
		t.Errorf("Value of ErrNilHandler is different. Expected: %d, Actual: %d", 3, ErrNilHandler)
	}

	if ErrInternalError != 4 {
		t.Errorf("Value of ErrInternalError is different. Expected: %d, Actual: %d", 4, ErrInternalError)
	}
}

// TestValidateMethod tests the validation of HTTP methods
func TestValidateMethod(t *testing.T) {
	// Valid HTTP methods
	validMethods := []string{
		"GET",
		"POST",
		"PUT",
		"DELETE",
		"PATCH",
		"HEAD",
		"OPTIONS",
	}

	// Invalid HTTP methods
	invalidMethods := []string{
		"",
		"INVALID",
		"get", // Lowercase is invalid
		"CONNECT",
		"TRACE",
	}

	// Test valid methods
	for _, method := range validMethods {
		err := validateMethod(method)
		if err != nil {
			t.Errorf("Valid method %s was determined to be invalid: %v", method, err)
		}
	}

	// Test invalid methods
	for _, method := range invalidMethods {
		err := validateMethod(method)
		if err == nil {
			t.Errorf("Invalid method %s was determined to be valid", method)
		}

		// Check error type
		routerErr, ok := err.(*RouterError)
		if !ok {
			t.Errorf("Not the expected error type: %T", err)
			continue
		}

		if routerErr.Code != ErrInvalidMethod {
			t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidMethod, routerErr.Code)
		}
	}
}

// TestValidatePattern tests the validation of route patterns
func TestValidatePattern(t *testing.T) {
	// Valid patterns
	validPatterns := []string{
		"/",
		"/users",
		"/users/{id}",
		"/users/{id}/profile",
		"/users/{id:[0-9]+}",
		"/api/v1/users",
	}

	// Invalid patterns
	invalidPatterns := []string{
		"", // Empty string
	}

	// Test valid patterns
	for _, pattern := range validPatterns {
		err := validatePattern(pattern)
		if err != nil {
			t.Errorf("Valid pattern %s was determined to be invalid: %v", pattern, err)
		}
	}

	// Test invalid patterns
	for _, pattern := range invalidPatterns {
		err := validatePattern(pattern)
		if err == nil {
			t.Errorf("Invalid pattern %s was determined to be valid", pattern)
		}

		// Check error type
		routerErr, ok := err.(*RouterError)
		if !ok {
			t.Errorf("Not the expected error type: %T", err)
			continue
		}

		if routerErr.Code != ErrInvalidPattern {
			t.Errorf("Error code is different. Expected: %d, Actual: %d", ErrInvalidPattern, routerErr.Code)
		}
	}
}

// TestParseSegments tests the parsing of path segments
func TestParseSegments(t *testing.T) {
	// Test cases
	tests := []struct {
		path           string
		expectedResult []string
	}{
		{"/", []string{""}},
		{"/users", []string{"users"}},
		{"/users/profile", []string{"users", "profile"}},
		{"/users/{id}", []string{"users", "{id}"}},
		{"/users/{id}/profile", []string{"users", "{id}", "profile"}},
		{"/api/v1/users", []string{"api", "v1", "users"}},
	}

	// Execute each test case
	for _, tt := range tests {
		result := parseSegments(tt.path)

		// Check the length of the result
		if len(result) != len(tt.expectedResult) {
			t.Errorf("Number of segments for path %s is different. Expected: %d, Actual: %d", tt.path, len(tt.expectedResult), len(result))
			continue
		}

		// Check each segment
		for i, expected := range tt.expectedResult {
			if result[i] != expected {
				t.Errorf("Segment %d for path %s is different. Expected: %s, Actual: %s", i, tt.path, expected, result[i])
			}
		}
	}
}

// TestIsAllStatic tests whether all segments are static
func TestIsAllStatic(t *testing.T) {
	// Test cases
	tests := []struct {
		segments       []string
		expectedResult bool
	}{
		{[]string{""}, true},
		{[]string{"users"}, true},
		{[]string{"users", "profile"}, true},
		{[]string{"users", "{id}"}, false},
		{[]string{"users", "{id}", "profile"}, false},
		{[]string{"users", "{id:[0-9]+}"}, false},
		{[]string{"api", "v1", "users"}, true},
	}

	// Execute each test case
	for _, tt := range tests {
		result := isAllStatic(tt.segments)

		// Check the result
		if result != tt.expectedResult {
			t.Errorf("Static determination for segments %v is different. Expected: %t, Actual: %t", tt.segments, tt.expectedResult, result)
		}
	}
}
