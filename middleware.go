package router

// MiddlewareFunc is a function type that takes a handler function and returns a new handler function.
// It is used to insert common processing before and after request processing.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// cleanupMiddleware is the implementation of CleanupMiddleware interface.
type cleanupMiddleware struct {
	mw      MiddlewareFunc
	cleanup func() error
}

// Use adds one or more middleware functions to the router.
// Middleware functions are executed before all route handlers, allowing for common processing such as authentication and logging.
func (r *Router) Use(mw ...MiddlewareFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// get current middleware list
	currentMiddleware := r.middleware.Load().([]MiddlewareFunc)

	// Create new middleware list (existing + new)
	newMiddleware := make([]MiddlewareFunc, len(currentMiddleware)+len(mw))
	copy(newMiddleware, currentMiddleware)
	copy(newMiddleware[len(currentMiddleware):], mw)

	// Atomic update
	r.middleware.Store(newMiddleware)
}

// AddCleanupMiddleware adds a cleanupable middleware to the router.
// This middleware is cleaned up when the Shutdown method is called.
func (r *Router) AddCleanupMiddleware(cm cleanupMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// get current middleware list
	currentMiddleware := r.middleware.Load().([]MiddlewareFunc)

	// Create new middleware list (existing + new)
	newMiddleware := make([]MiddlewareFunc, len(currentMiddleware)+1)
	copy(newMiddleware, currentMiddleware)
	newMiddleware[len(currentMiddleware)] = cm.Middleware()

	// Atomic update
	r.middleware.Store(newMiddleware)

	// Add to cleanup list
	currentCleanup := r.cleanupMws.Load().([]cleanupMiddleware)
	newCleanup := make([]cleanupMiddleware, len(currentCleanup)+1)
	copy(newCleanup, currentCleanup)
	newCleanup[len(currentCleanup)] = cm

	r.cleanupMws.Store(newCleanup)
}
