package router

import (
	"regexp"
	"strings"
)

// segmentType is a type representing the kind of URL path segment.
type segmentType uint8

// Constants defining segment types
const (
	staticSegment segmentType = iota // Static segment (normal string)
	paramSegment                     // Parameter segment ({name} format)
	regexSegment                     // Regular expression segment ({name:pattern} format)
)

// node represents a segment of a URL path.
// It forms a Radix tree structure and is used
// to efficiently manage route matching.
type node struct {
	segment     string         // Path segment this node represents
	handler     HandlerFunc    // Handler function associated with this node
	children    []*node        // List of child nodes
	segmentType segmentType    // Segment type (static, parameter, regular expression)
	regex       *regexp.Regexp // Regular expression pattern (used only when segType is regex)
}

// newNode creates and returns a new node.
// It parses the pattern and sets the appropriate segment type.
// It will panic if the regular expression pattern is invalid.
func newNode(pattern string) *node {
	n := &node{
		segment:  pattern,
		children: make([]*node, 0, 8), // set initial capacity to 8 (sufficient for common cases)
	}
	if err := n.parseSegment(); err != nil {
		panic(err)
	}
	return n
}

// addRoute adds a route pattern and handler to the tree.
// It processes path segments in order and creates new nodes as needed.
// Duplicate registration for the same path pattern results in an error.
// Different parameter names for the same path pattern (e.g., /users/{id} and /users/{name}) also result in an error.
// Conflicts in regular expression patterns are allowed and prioritized by registration order.
// Using the same parameter name multiple times in the same route (e.g., /users/{id}/posts/{id}) also results in an error.
func (n *node) addRoute(segments []string, handler HandlerFunc) error {
	// Map for checking duplicate parameter names
	return n.addRouteWithParamCheck(segments, handler, make(map[string]struct{}))
}

// addRouteWithParamCheck performs the actual route addition and checks for duplicate parameter names.
func (n *node) addRouteWithParamCheck(segments []string, handler HandlerFunc, usedParams map[string]struct{}) error {
	// If all segments have been processed, set the handler for the current node
	if len(segments) == 0 {
		if n.handler != nil {
			return &RouterError{Code: ErrInvalidPattern, Message: "duplicate pattern"}
		}
		n.handler = handler
		return nil
	}

	// get the current segment
	currentSegment := segments[0]

	// If it's a parameter segment, check for duplicate parameter names
	if isDynamicSeg(currentSegment) {
		paramName := extractParamName(currentSegment)
		if _, exists := usedParams[paramName]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate parameter name in route: " + paramName,
			}
		}
		// Record the parameter name as used
		usedParams[paramName] = struct{}{}
	}

	// search for existing child nodes
	child := n.findChild(currentSegment)

	// If a child node exists, check the segment type
	if child != nil {
		// Create a temporary node to get the segment type
		tempNode := newNode(currentSegment)

		// If the segment types are the same but the patterns are different, it's an error
		// Example: /users/{id} and /users/{name} conflict
		if tempNode.segmentType == paramSegment && child.segmentType == paramSegment && tempNode.segment != child.segment {
			// Extract parameter names
			tempParamName := extractParamName(tempNode.segment)
			childParamName := extractParamName(child.segment)

			if tempParamName != childParamName {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "conflicting parameter names in pattern: " + tempParamName + " and " + childParamName,
				}
			}
		}

		// Check for mixing static segments and dynamic segments
		if (tempNode.segmentType == staticSegment && (child.segmentType == paramSegment || child.segmentType == regexSegment)) ||
			((tempNode.segmentType == paramSegment || tempNode.segmentType == regexSegment) && child.segmentType == staticSegment) {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "conflicting segment types: static and dynamic segments cannot be mixed at the same position",
			}
		}

		// Recursively process the remaining segments
		return child.addRouteWithParamCheck(segments[1:], handler, usedParams)
	}

	// If no child node exists, create a new one
	child = newNode(currentSegment)
	n.children = append(n.children, child)

	// Recursively process the remaining segments
	return child.addRouteWithParamCheck(segments[1:], handler, usedParams)
}

// extractParamName extracts the parameter name from a parameter segment ({name} format).
func extractParamName(pattern string) string {
	// Assume the pattern is in {name} format
	if len(pattern) < 3 || pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		return ""
	}

	// If there's a colon, the part before the colon is the parameter name
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		return pattern[1:colonIdx]
	}

	// If there's no colon, the entire content inside the braces is the parameter name
	return pattern[1 : len(pattern)-1]
}

// match checks if the path matches this node or any of its child nodes.
// If it matches, it returns the handler function and true; if it doesn't, it returns nil and false.
// If parameters are extracted, they are added to params.
func (n *node) match(path string, params *Params) (HandlerFunc, bool) {
	// If the path is empty, return the handler for the current node
	if path == "" || path == "/" {
		return n.handler, true
	}

	// If the path starts with /, remove it
	if path[0] == '/' {
		path = path[1:]
	}

	// Extract the current segment and the remaining path
	var currentSegment string
	var remainingPath string

	slashIndex := strings.IndexByte(path, '/')
	if slashIndex == -1 {
		// If there's no slash, the entire path is the current segment
		currentSegment = path
		remainingPath = ""
	} else {
		// If there's a slash, the part before the slash is the current segment
		currentSegment = path[:slashIndex]
		remainingPath = path[slashIndex:]
	}

	// Classify child nodes
	var staticMatches []*node
	var paramMatches []*node
	var regexMatches []*node

	// Classify child nodes in one loop
	for _, child := range n.children {
		if child.segmentType == staticSegment && child.segment == currentSegment {
			staticMatches = append(staticMatches, child)
		} else if child.segmentType == paramSegment {
			paramMatches = append(paramMatches, child)
		} else if child.segmentType == regexSegment && child.regex.MatchString(currentSegment) {
			regexMatches = append(regexMatches, child)
		}
	}

	// match static segments first
	for _, child := range staticMatches {
		handler, matched := child.match(remainingPath, params)
		if matched {
			return handler, true
		}
	}

	// match parameter segments
	for _, child := range paramMatches {
		// Extract parameter name
		paramName := extractParamName(child.segment)
		// Add parameter
		params.Add(paramName, currentSegment)
		handler, matched := child.match(remainingPath, params)
		if matched {
			return handler, true
		}
		// If no match, remove parameter (backtracking)
		// Current implementation does not remove, uses overwrite method
	}

	// match regular expression segments
	for _, child := range regexMatches {
		// Extract parameter name
		paramName := extractParamName(child.segment)
		// Add parameter
		params.Add(paramName, currentSegment)
		handler, matched := child.match(remainingPath, params)
		if matched {
			return handler, true
		}
		// If no match, remove parameter (backtracking)
		// Current implementation does not remove, uses overwrite method
	}

	// No matching node found
	return nil, false
}

// parseSegment parses the pattern string and determines the segment type.
// It also compiles the regexp pattern if it's a regular expression segment.
// It returns an error if the regular expression pattern is invalid.
func (n *node) parseSegment() error {
	pattern := n.segment

	// Empty pattern is a static segment
	if pattern == "" {
		n.segmentType = staticSegment
		return nil
	}

	// Check if it's a parameter format ({param} or {param:regex})
	if pattern[0] != '{' || pattern[len(pattern)-1] != '}' {
		n.segmentType = staticSegment
		return nil
	}

	// Regular expression pattern detection ({name:pattern} format)
	if colonIdx := strings.IndexByte(pattern, ':'); colonIdx > 0 {
		n.segmentType = regexSegment
		regexStr := pattern[colonIdx+1 : len(pattern)-1]

		// Compile regular expression (add ^ and $ automatically to ensure full match)
		// If ^ and $ are already included, don't add
		var completeRegexStr string
		if !strings.HasPrefix(regexStr, "^") {
			completeRegexStr = "^" + regexStr
		} else {
			completeRegexStr = regexStr
		}
		if !strings.HasSuffix(regexStr, "$") {
			completeRegexStr = completeRegexStr + "$"
		}

		var err error
		n.regex, err = regexp.Compile(completeRegexStr)
		if err != nil {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "invalid regex pattern: " + regexStr + " - " + err.Error(),
			}
		}
		return nil
	}

	// Simple parameter ({name} format)
	n.segmentType = paramSegment
	return nil
}

// findChild searches for a child node that matches the given pattern.
// It returns the node if a fully matching child node exists; otherwise, it returns nil.
// If there are many child nodes, a map is used for faster lookup.
func (n *node) findChild(pattern string) *node {
	// If there are few child nodes, linear search (most common case)
	if len(n.children) < 8 {
		for _, child := range n.children {
			if child.segment == pattern {
				return child
			}
		}
		return nil
	}

	// If there are many child nodes, use a map for faster lookup
	childMap := make(map[string]*node, len(n.children))
	for _, child := range n.children {
		childMap[child.segment] = child
	}

	return childMap[pattern]
}

// removeRoute removes the route that matches the specified segment path.
// It returns true if the removed route existed; otherwise, it returns false.
func (n *node) removeRoute(segments []string) bool {
	return n.removeRouteInternal(segments, 0, make(map[string]struct{}))
}

// removeRouteInternal is the internal implementation of removeRoute.
// It recursively processes segments and removes matching routes.
func (n *node) removeRouteInternal(segments []string, index int, paramNames map[string]struct{}) bool {
	// If the last segment is reached
	if index >= len(segments) {
		// If a handler exists, remove it and return true
		if n.handler != nil {
			n.handler = nil
			return true
		}
		return false
	}

	segment := segments[index]

	// search for child nodes
	for i, child := range n.children {
		// If it's a static segment, check for full match
		if child.segmentType == staticSegment && child.segment == segment {
			// Recursively attempt to remove
			removed := child.removeRouteInternal(segments, index+1, paramNames)

			// If the child node's handler and child nodes are gone, remove the child node itself
			if removed && child.handler == nil && len(child.children) == 0 {
				n.children = append(n.children[:i], n.children[i+1:]...)
			}

			return removed
		}

		// If it's a parameter segment or regular expression segment
		if (child.segmentType == paramSegment || child.segmentType == regexSegment) &&
			(segment[0] == '{' && segment[len(segment)-1] == '}') {
			// Recursively attempt to remove
			removed := child.removeRouteInternal(segments, index+1, paramNames)

			// If the child node's handler and child nodes are gone, remove the child node itself
			if removed && child.handler == nil && len(child.children) == 0 {
				n.children = append(n.children[:i], n.children[i+1:]...)
			}

			return removed
		}
	}

	// No matching node found
	return false
}
