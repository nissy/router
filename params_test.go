package router

import (
	"context"
	"testing"
)

// TestParamsCreation tests the creation of Params
func TestParamsCreation(t *testing.T) {
	// Create a new Params
	params := NewParams()

	// Check initial state
	if params == nil {
		t.Fatalf("Failed to create Params")
	}

	if params.Len() != 0 {
		t.Errorf("Initial size of Params is different. Expected: %d, Actual: %d", 0, params.Len())
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestParamsAddAndGet tests adding and retrieving parameters
func TestParamsAddAndGet(t *testing.T) {
	// Create a new Params
	params := NewParams()

	// Add parameters
	params.Add("id", "123")
	params.Add("name", "test")

	// Check the number of parameters
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

	// Return parameters to the pool
	PutParams(params)
}

// TestParamsReset tests resetting parameters
func TestParamsReset(t *testing.T) {
	// Create a new Params
	params := NewParams()

	// Add parameters
	params.Add("id", "123")
	params.Add("name", "test")

	// Check the number of parameters
	if params.Len() != 2 {
		t.Errorf("Number of parameters is different. Expected: %d, Actual: %d", 2, params.Len())
	}

	// Reset parameters
	params.reset()

	// Check the number of parameters after reset
	if params.Len() != 0 {
		t.Errorf("Number of parameters after reset is different. Expected: %d, Actual: %d", 0, params.Len())
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestParamsPool tests the parameter pool
func TestParamsPool(t *testing.T) {
	// Create and return multiple Params
	for range make([]struct{}, 10) {
		params := NewParams()
		params.Add("id", "123")
		PutParams(params)
	}

	// Get a reused Params from the pool
	params := NewParams()

	// Verify that the reused Params is empty
	if params.Len() != 0 {
		t.Errorf("Reused Params is not empty. Size: %d", params.Len())
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestParamsCapacity tests the capacity of parameters
func TestParamsCapacity(t *testing.T) {
	// Create a new Params
	params := NewParams()

	// Add many parameters
	for i := 0; i < 100; i++ {
		params.Add("key"+string(rune('0'+i%10)), "value"+string(rune('0'+i%10)))
	}

	// Check the number of parameters
	if params.Len() != 100 {
		t.Errorf("Number of parameters is different. Expected: %d, Actual: %d", 100, params.Len())
	}

	// Return parameters to the pool
	PutParams(params)
}

// TestContextWithParams tests adding parameters to context
func TestContextWithParams(t *testing.T) {
	// Create a new Params
	params := NewParams()
	params.Add("id", "123")

	// Add parameters to context
	ctx := context.Background()
	ctx = contextWithParams(ctx, params)

	// Get parameters from context
	retrievedParams := GetParams(ctx)

	// Check parameters
	if retrievedParams == nil {
		t.Fatalf("Failed to retrieve parameters from context")
	}

	if val, ok := retrievedParams.Get("id"); !ok || val != "123" {
		t.Errorf("Value of parameter id is different. Expected: %s, Actual: %s", "123", val)
	}
}

// TestGetParamsWithNilContext tests retrieving parameters from a nil context
func TestGetParamsWithNilContext(t *testing.T) {
	// Use context.TODO() instead of nil context
	params := GetParams(context.TODO())

	// Verify that parameters are newly created
	if params == nil {
		t.Fatalf("Failed to retrieve parameters from empty context")
	}

	if params.Len() != 0 {
		t.Errorf("Newly created parameters are not empty. Size: %d", params.Len())
	}
}

// TestGetParamsWithEmptyContext tests retrieving parameters from an empty context
func TestGetParamsWithEmptyContext(t *testing.T) {
	// Get parameters from an empty context
	ctx := context.Background()
	params := GetParams(ctx)

	// Verify that parameters are newly created
	if params == nil {
		t.Fatalf("Failed to retrieve parameters from empty context")
	}

	if params.Len() != 0 {
		t.Errorf("Newly created parameters are not empty. Size: %d", params.Len())
	}
}
