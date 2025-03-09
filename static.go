package router

import (
	"math"
	"sync"
)

// DoubleArrayTrie is a data structure that enables fast string matching.
// Each node is represented by an array, using base and check values to manage transitions.
// It specializes in searching static route patterns, balancing memory efficiency and search speed.
type DoubleArrayTrie struct {
	base    []int32       // Base value for each node. Used for transitions to child nodes
	check   []int32       // Used to verify parent-child relationships. 0 indicates unused
	handler []HandlerFunc // Handler functions associated with each node
	size    int32         // Number of nodes in use
	mu      sync.RWMutex  // Mutex for protection from concurrent access
}

// Constants
const (
	initialTrieSize = 1024       // Initial size of the trie
	growthFactor    = 1.5        // Growth factor when expanding
	baseOffset      = int32(256) // Offset value for base array
	rootNode        = int32(0)   // Index of the root node
)

// newDoubleArrayTrie initializes and returns a new DoubleArrayTrie instance.
// It allocates arrays with the initial size and sets the base value for the root node.
func newDoubleArrayTrie() *DoubleArrayTrie {
	t := &DoubleArrayTrie{
		base:    make([]int32, initialTrieSize),
		check:   make([]int32, initialTrieSize),
		handler: make([]HandlerFunc, initialTrieSize),
		size:    1, // Root node exists, so start from 1
	}

	// Set the base value for the root node
	t.base[rootNode] = baseOffset
	return t
}

// Add adds a path and handler function to the trie.
// Returns an error if the same path is already registered.
func (t *DoubleArrayTrie) Add(path string, h HandlerFunc) error {
	if len(path) == 0 {
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "empty path is not allowed",
		}
	}

	// Check for nil handler
	if h == nil {
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "nil handler is not allowed",
		}
	}

	// Check if the path already exists (executed outside the lock)
	var existingHandler HandlerFunc
	t.mu.RLock()
	existingHandler = t.searchWithoutLock(path)
	t.mu.RUnlock()

	if existingHandler != nil {
		// The Router.Handle method already checks for duplicates,
		// so this error should not normally occur, but is implemented for safety.
		return &RouterError{
			Code:    ErrInvalidPattern,
			Message: "duplicate static route: " + path,
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	// Process the path character by character
	currentNode := rootNode
	for i := 0; i < len(path); i++ {
		c := path[i]
		baseVal := t.base[currentNode]

		// If the current node doesn't have any child nodes yet
		if baseVal == 0 {
			// Calculate the new base value
			nextNode := currentNode + int32(c) + 1

			// Expand the base array if needed
			if nextNode >= int32(len(t.base)) {
				// Calculate array size (at least double, or larger if needed)
				newSize := int32(len(t.base)) * 2
				if nextNode >= newSize {
					newSize = nextNode + 1024 // Add extra space
				}

				// Expand the array
				if err := t.expand(newSize); err != nil {
					return err
				}
			}

			// Set the new transition
			t.base[currentNode] = nextNode - int32(c)
			t.check[nextNode] = currentNode
			currentNode = nextNode
		} else {
			// Calculate the next node using the existing base value
			nextNode := baseVal + int32(c)

			// Expand the base array if needed
			if nextNode >= int32(len(t.base)) {
				// Calculate array size (at least double, or larger if needed)
				newSize := int32(len(t.base)) * 2
				if nextNode >= newSize {
					newSize = nextNode + 1024 // Add extra space
				}

				// Expand the array
				if err := t.expand(newSize); err != nil {
					return err
				}
			}

			// Check if the transition destination is unused
			if t.check[nextNode] == 0 {
				// If unused, set it
				t.check[nextNode] = currentNode
				currentNode = nextNode
			} else if t.check[nextNode] == currentNode {
				// If already transitioning from the same parent with the same character, no problem
				currentNode = nextNode
			} else {
				// If a collision occurs, find a new base value
				newBase := t.findBase([]byte(path[i:]))
				if newBase < 0 {
					return &RouterError{
						Code:    ErrInternalError,
						Message: "failed to find new base value",
					}
				}

				// Move existing child nodes to new positions
				oldBase := t.base[currentNode]
				for ch := byte(0); ch < 128; ch++ { // Support ASCII characters only
					oldNext := oldBase + int32(ch)
					if oldNext < int32(len(t.check)) && t.check[oldNext] == currentNode {
						// Found an existing child node
						newNext := newBase + int32(ch)

						// Expand the array if needed
						if newNext >= int32(len(t.base)) {
							newSize := int32(len(t.base)) * 2
							if newNext >= newSize {
								newSize = newNext + 1024
							}
							if err := t.expand(newSize); err != nil {
								return err
							}
						}

						// Move the child node to the new position
						t.base[newNext] = t.base[oldNext]
						t.check[newNext] = currentNode

						// Clear the old position
						t.check[oldNext] = 0
					}
				}

				// Update the base of the current node
				t.base[currentNode] = newBase

				// Add the new transition
				nextNode = newBase + int32(c)
				t.check[nextNode] = currentNode
				currentNode = nextNode
			}
		}
	}

	// Set the handler at the terminal node
	if int(currentNode) >= len(t.handler) {
		// Expand the handler array as well
		newHandlers := make([]HandlerFunc, len(t.base))
		copy(newHandlers, t.handler)
		t.handler = newHandlers
	}
	t.handler[currentNode] = h

	// Update the number of nodes in use
	if currentNode >= t.size {
		t.size = currentNode + 1
	}

	return nil
}

// searchWithoutLock searches for a path without locking.
// Intended for internal use only.
func (t *DoubleArrayTrie) searchWithoutLock(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}

	// Start from the root node
	currentNode := rootNode

	// Process the path character by character
	for i := 0; i < len(path); i++ {
		c := path[i]

		// Calculate the next node
		if t.base[currentNode] == 0 {
			return nil // No matching path
		}

		nextNode := t.base[currentNode] + int32(c)

		// Check if the transition is valid
		if t.check[nextNode] != currentNode {
			return nil // No matching path
		}

		currentNode = nextNode
	}

	// Check if there is a handler at the terminal node
	if int(currentNode) < len(t.handler) && t.handler[currentNode] != nil {
		return t.handler[currentNode]
	}

	return nil
}

// Search searches for a handler function that matches the path.
// Returns nil if no matching path is found.
func (t *DoubleArrayTrie) Search(path string) HandlerFunc {
	if len(path) == 0 {
		return nil
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	return t.searchWithoutLock(path)
}

// findBase searches for an appropriate base value for the specified character set.
// It searches until it finds a position with no conflicts for all characters in the character set.
func (t *DoubleArrayTrie) findBase(suffix []byte) int32 {
	// Get the maximum character code in the suffix
	var maxCharCode int32 = 0
	for _, char := range suffix {
		if int32(char) > maxCharCode {
			maxCharCode = int32(char)
		}
	}

	// Start with a base value candidate of 1
	baseCandidate := int32(1)

	// Search until a base value with no conflicts is found
	for {
		// Calculate the required array size
		requiredSize := baseCandidate + maxCharCode + 1

		// If array size expansion is needed
		if requiredSize > int32(len(t.check)) {
			if err := t.expand(requiredSize); err != nil {
				return -1
			}
		}

		// Check for conflicts
		hasCollision := false
		for _, char := range suffix {
			nextPos := baseCandidate + int32(char)
			if t.check[nextPos] != 0 { // Position already in use
				hasCollision = true
				break
			}
		}

		// Use this base value if there are no conflicts
		if !hasCollision {
			return baseCandidate
		}

		// Try the next candidate
		baseCandidate++
	}
}

// expand expands the array size of the trie.
// The new size is calculated as a multiple of the current size.
func (t *DoubleArrayTrie) expand(requiredSize int32) error {
	// Calculate the new size (either a multiple of the current size or the required size, whichever is larger)
	newSize := int32(math.Max(float64(len(t.base))*growthFactor, float64(requiredSize)))

	// Check size limit
	if newSize > 1<<30 { // About 1 billion nodes
		return &RouterError{Code: ErrInternalError, Message: "trie size limit exceeded"}
	}

	// Create a new array
	newBase := make([]int32, newSize)
	newCheck := make([]int32, newSize)
	newHandler := make([]HandlerFunc, newSize)

	// Copy existing data
	copy(newBase, t.base)
	copy(newCheck, t.check)
	if t.handler != nil {
		copy(newHandler, t.handler)
	}

	// Set new array
	t.base = newBase
	t.check = newCheck
	t.handler = newHandler

	return nil
}
