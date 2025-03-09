package router

import (
	"log"
	"net/http"
	"strings"
	"time"
)

// applyMiddlewareChain applies middleware chain to a handler function.
// Middleware is applied in the order of registration (first registered is executed first).
func applyMiddlewareChain(h HandlerFunc, middleware []MiddlewareFunc) HandlerFunc {
	// Apply middleware in registration order
	for i := 0; i < len(middleware); i++ {
		h = middleware[i](h)
	}
	return h
}

// Route represents a single route.
// It provides an interface for applying middleware.
type Route struct {
	group        *Group                                          // Group this route belongs to (nil if not part of a group)
	router       *Router                                         // Router this route belongs to
	method       string                                          // HTTP method
	subPath      string                                          // Route path
	handler      HandlerFunc                                     // Handler function
	middleware   []MiddlewareFunc                                // List of middleware functions
	applied      bool                                            // Whether already applied
	timeout      time.Duration                                   // Route-specific timeout setting (uses router default if 0)
	errorHandler func(http.ResponseWriter, *http.Request, error) // Route-specific error handler
}

// WithMiddleware is used to apply specific middleware to a route.
// Middleware is applied to the handler function and the same Route object is returned.
func (r *Route) WithMiddleware(middleware ...MiddlewareFunc) *Route {
	// If the route has already been applied, return it as is
	if r.applied {
		return r
	}

	// Add middleware
	r.middleware = append(r.middleware, middleware...)

	return r
}

// build registers the route with the router.
// This method must be explicitly called.
// If duplicate routes are detected, an error is returned.
func (r *Route) build() error {
	if r.applied {
		return nil
	}

	// Apply middleware to the handler
	handler := r.handler
	if len(r.middleware) > 0 {
		handler = applyMiddlewareChain(handler, r.middleware)
	}

	var err error

	// If the route does not belong to a group (created by router.Route)
	if r.group == nil {
		// Register route directly with the router
		err = r.router.Handle(r.method, r.subPath, handler)
	} else {
		// If the route belongs to a group
		fullPath := joinPath(r.group.prefix, normalizePath(r.subPath))
		err = r.router.Handle(r.method, fullPath, handler)
	}

	// If there is no error, set applied flag
	if err == nil {
		r.applied = true
	}

	return err
}

type Group struct {
	router       *Router
	prefix       string
	middleware   []MiddlewareFunc
	routes       []*Route
	timeout      time.Duration                                   // Group-specific timeout setting (uses router default if 0)
	errorHandler func(http.ResponseWriter, *http.Request, error) // Group-specific error handler
}

// Group creates a new route group.
// It returns a Group with the specified path prefix.
func (r *Router) Group(prefix string, middleware ...MiddlewareFunc) *Group {
	group := &Group{
		router:       r,
		prefix:       normalizePath(prefix),
		middleware:   middleware,
		routes:       make([]*Route, 0),
		timeout:      0,
		errorHandler: nil,
	}

	// Add group to the router
	r.groups = append(r.groups, group)

	return group
}

// Group creates a new route group.
// The new group inherits the path prefix and middleware of the parent group and
// applies additional path prefix and middleware.
func (g *Group) Group(prefix string, middleware ...MiddlewareFunc) *Group {
	// Combine parent group's middleware and new middleware
	combinedMiddleware := make([]MiddlewareFunc, len(g.middleware)+len(middleware))
	copy(combinedMiddleware, g.middleware)
	copy(combinedMiddleware[len(g.middleware):], middleware)

	return &Group{
		router:     g.router,
		prefix:     joinPath(g.prefix, normalizePath(prefix)),
		middleware: combinedMiddleware,
		routes:     make([]*Route, 0),
	}
}

// Use adds new middleware to the group.
func (g *Group) Use(middleware ...MiddlewareFunc) *Group {
	g.middleware = append(g.middleware, middleware...)
	return g
}

// Handle is the implementation of routerGroup's Handle method.
// It registers a route with the specified HTTP method, pattern, and handler function.
// The pattern automatically includes the group's prefix,
// and the handler function is applied the group's middleware.
func (g *Group) Handle(method, subPath string, h HandlerFunc) error {
	full := joinPath(g.prefix, normalizePath(subPath))

	// Apply group's middleware to the handler
	h = applyMiddlewareChain(h, g.middleware)

	return g.router.Handle(method, full, h)
}

// Route creates a new route but does not register it.
// You can call WithMiddleware on the returned Route object to apply specific middleware.
// Route duplication processing is determined by the router's allowRouteOverride option:
// - true: The later registered route overwrites the existing route.
// - false: If duplicate routes are detected, an error is returned (default)
func (g *Group) Route(method, subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	// Check existing routes
	normalizedPath := normalizePath(subPath)

	// Duplicate check
	for i, existingRoute := range g.routes {
		if existingRoute.method == method && existingRoute.subPath == normalizedPath {
			// Duplicate found
			if !g.router.allowRouteOverride {
				// Output warning log (error is not returned - will be detected at build time unless overridden)
				log.Printf("Warning: duplicate route definition in group: %s %s%s (will cause error at build time unless overridden)",
					method, g.prefix, normalizedPath)
			} else {
				// Overwrite mode case
				g.routes[i] = &Route{
					group:        g,
					router:       g.router,
					method:       method,
					subPath:      normalizedPath,
					handler:      h,
					middleware:   make([]MiddlewareFunc, 0, len(middleware)),
					applied:      false,
					timeout:      g.timeout,
					errorHandler: nil,
				}

				// Add middleware
				if len(middleware) > 0 {
					g.routes[i].middleware = append(g.routes[i].middleware, middleware...)
				}

				return g.routes[i]
			}
		}
	}

	// Create new route
	route := &Route{
		group:        g,
		router:       g.router,
		method:       method,
		subPath:      normalizedPath,
		handler:      h,
		middleware:   make([]MiddlewareFunc, 0, len(middleware)),
		applied:      false,
		timeout:      g.timeout,
		errorHandler: nil,
	}

	// Add middleware
	if len(middleware) > 0 {
		route.middleware = append(route.middleware, middleware...)
	}

	// Add route to group
	g.routes = append(g.routes, route)

	return route
}

// Build registers all routes in the group.
// This method must be explicitly called.
// If duplicate routes are detected, an error is returned.
// Note: This method is usually called from Router.Build.
func (g *Group) Build() error {
	// Local duplicate check map (only check within group)
	routeMap := make(map[string]struct{})

	for _, route := range g.routes {
		if route.applied {
			continue
		}

		// Calculate full path
		fullPath := joinPath(g.prefix, route.subPath)

		// Local duplicate check
		routeKey := route.method + ":" + fullPath
		if _, exists := routeMap[routeKey]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate route definition in group: " + route.method + " " + fullPath,
			}
		}
		routeMap[routeKey] = struct{}{}

		if err := route.build(); err != nil {
			return err
		}
	}
	return nil
}

// Get creates a GET route.
// You can call WithMiddleware on the returned Route object to apply specific middleware.
func (g *Group) Get(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodGet, subPath, h, middleware...)
	return route
}

// Post creates a POST route.
func (g *Group) Post(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPost, subPath, h, middleware...)
	return route
}

// Put creates a PUT route.
func (g *Group) Put(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPut, subPath, h, middleware...)
	return route
}

// Delete creates a DELETE route.
func (g *Group) Delete(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodDelete, subPath, h, middleware...)
	return route
}

// Patch creates a PATCH route.
func (g *Group) Patch(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodPatch, subPath, h, middleware...)
	return route
}

// Head creates a HEAD route.
func (g *Group) Head(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodHead, subPath, h, middleware...)
	return route
}

// Options creates an OPTIONS route.
func (g *Group) Options(subPath string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	route := g.Route(http.MethodOptions, subPath, h, middleware...)
	return route
}

// WithTimeout sets a specific timeout value for the group.
// This applies to all routes in the group (except for routes with specific settings)
func (g *Group) WithTimeout(timeout time.Duration) *Group {
	g.timeout = timeout
	return g
}

// GetTimeout returns the group's timeout setting.
// If the group has no specific setting, the router's default value is returned.
func (g *Group) GetTimeout() time.Duration {
	if g.timeout <= 0 {
		return g.router.GetRequestTimeout()
	}
	return g.timeout
}

// WithErrorHandler sets a specific error handler for the group.
// This applies to all routes in the group (except for routes with specific settings)
func (g *Group) WithErrorHandler(handler func(http.ResponseWriter, *http.Request, error)) *Group {
	g.errorHandler = handler
	return g
}

// GetErrorHandler returns the group's error handler.
// If the group has no specific setting, the router's default value is returned.
func (g *Group) GetErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	if g.errorHandler != nil {
		return g.errorHandler
	}
	return g.router.GetErrorHandler() // router's GetErrorHandler returns defaultErrorHandler if nil
}

func normalizePath(path string) string {
	if path == "" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	// If the path ends with "/" and is not a route, remove it
	if len(path) > 1 && path[len(path)-1] == '/' {
		path = path[:len(path)-1]
	}
	return path
}

func joinPath(p1, p2 string) string {
	if p1 == "/" {
		return p2
	}
	return p1 + p2
}

// WithTimeout sets a specific timeout value for the route.
// If the timeout is 0 or less, the router's default value is used.
func (r *Route) WithTimeout(timeout time.Duration) *Route {
	// If the route has already been applied, return it as is
	if r.applied {
		return r
	}

	// Set timeout
	r.timeout = timeout

	return r
}

// GetTimeout returns the route's timeout setting.
// If the route has no specific setting, the router's default value is returned.
func (r *Route) GetTimeout() time.Duration {
	if r.timeout <= 0 {
		return r.router.GetRequestTimeout()
	}
	return r.timeout
}

// WithErrorHandler sets a specific error handler for the route.
// If the error handler is nil, the default value of the group or router is used.
func (r *Route) WithErrorHandler(handler func(http.ResponseWriter, *http.Request, error)) *Route {
	// If the route has already been applied, return it as is
	if r.applied {
		return r
	}

	// Set error handler
	r.errorHandler = handler

	return r
}

// GetErrorHandler returns the route's error handler.
// If the route has no specific setting, the default value of the group or router is returned.
// If all are nil, the default error handler is returned.
func (r *Route) GetErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	if r.errorHandler != nil {
		return r.errorHandler
	}
	if r.group != nil && r.group.GetErrorHandler() != nil {
		return r.group.GetErrorHandler()
	}
	return r.router.GetErrorHandler() // router's GetErrorHandler returns defaultErrorHandler if nil
}
