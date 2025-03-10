package router

import (
	"context"
	"log"
	"net/http"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// responseWriter is an extension of http.ResponseWriter that tracks the write status of the response.
type responseWriter struct {
	http.ResponseWriter
	written bool
	status  int
}

// WriteHeader sets the HTTP status code.
// It does nothing if the response has already been written.
func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.status = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

// Write writes the response body.
// Writing is tracked by setting the written flag.
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Written returns whether the response has already been written.
func (rw *responseWriter) Written() bool {
	return rw.written
}

// Status returns the set HTTP status code.
func (rw *responseWriter) Status() int {
	return rw.status
}

// HandlerFunc is a function type for processing HTTP requests and returning an error.
// Unlike the standard http.HandlerFunc, it allows returning an error for error handling.
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// MiddlewareFunc is a function type that takes a handler function and returns a new handler function.
// It is used to insert common processing before and after request processing.
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// CleanupMiddleware is an interface for cleanupable middleware.
type CleanupMiddleware interface {
	Cleanup() error
	Middleware() MiddlewareFunc
}

// cleanupMiddlewareImpl is the implementation of CleanupMiddleware interface.
type cleanupMiddlewareImpl struct {
	mw      MiddlewareFunc
	cleanup func() error
}

// Cleanup implements the CleanupMiddleware interface.
func (c *cleanupMiddlewareImpl) Cleanup() error {
	if c.cleanup != nil {
		return c.cleanup()
	}
	return nil
}

// Middleware implements the CleanupMiddleware interface.
func (c *cleanupMiddlewareImpl) Middleware() MiddlewareFunc {
	return c.mw
}

// NewCleanupMiddleware creates a new CleanupMiddleware.
func NewCleanupMiddleware(mw MiddlewareFunc, cleanup func() error) CleanupMiddleware {
	return &cleanupMiddlewareImpl{
		mw:      mw,
		cleanup: cleanup,
	}
}

// RouterOptions are options to set up the router's behavior.
type RouterOptions struct {
	// AllowRouteOverride specifies how to handle duplicate route registration.
	// true: The later registered route overwrites the existing route.
	// false: If a duplicate route is detected, an error is returned (default).
	AllowRouteOverride bool

	// RequestTimeout is the default timeout time for request processing.
	// A value of 0 or less disables the timeout.
	// Default: 0 seconds (no timeout)
	RequestTimeout time.Duration

	// CacheMaxEntries is the maximum number of entries in the route cache.
	// Default: 1000
	CacheMaxEntries int
}

// DefaultRouterOptions returns the default router options.
func DefaultRouterOptions() RouterOptions {
	return RouterOptions{
		AllowRouteOverride: false,
		RequestTimeout:     0 * time.Second, // no timeout
		CacheMaxEntries:    defaultCacheMaxEntries,
	}
}

// Router is the main structure for managing HTTP request routing.
// It supports both static routes (DoubleArrayTrie) and dynamic routes (Radix tree),
// providing high-speed route matching and caching mechanism.
type Router struct {
	// Routing-related
	staticTrie   *DoubleArrayTrie // High-speed trie structure for static routes
	dynamicNodes [8]*Node         // Radix tree for dynamic routes for each HTTP method (index corresponds to methodToUint8)
	cache        *Cache           // Cache route matching results for performance
	routes       []*Route         // Directly registered routes
	groups       []*Group         // Registered groups

	// Handler-related
	// 各ハンドラーは異なる状況や目的に対応するために個別に存在しています：
	// - errorHandler: ルートハンドラー内で発生したエラーを処理します（アプリケーションロジックのエラー）
	// - shutdownHandler: サーバーがシャットダウン中の場合のリクエスト処理を担当します
	// - timeoutHandler: リクエスト処理がタイムアウトした場合の処理を担当します
	// - notFoundHandler: 存在しないルートへのリクエストを処理します
	// これらを分離することで、各状況に応じた適切な処理を個別に定義でき、コードの保守性と拡張性が向上します。
	errorHandler    func(http.ResponseWriter, *http.Request, error) // Error handling function
	shutdownHandler http.HandlerFunc                                // Request processing function during shutdown
	timeoutHandler  http.HandlerFunc                                // Timeout handling function
	notFoundHandler http.HandlerFunc                                // Not found handler

	// Middleware-related
	middleware atomic.Value // List of middleware functions (atomic.Value used for thread-safe updates)
	cleanupMws atomic.Value // List of cleanupable middleware

	// Synchronization-related
	mu             sync.RWMutex   // Mutex for protection from concurrent access
	activeRequests sync.WaitGroup // Track the number of active requests
	wgMu           sync.Mutex     // Mutex for protecting access to activeRequests
	shuttingDown   atomic.Bool    // Flag indicating whether shutting down

	// Timeout settings
	requestTimeout time.Duration // Request processing timeout time (0 means no timeout)
	timeoutMu      sync.RWMutex  // Mutex for protecting access to timeout settings

	// Parameter-related
	paramsPool *ParamsPool // URL parameter object pool (specific to each router instance)

	// Configuration options
	allowRouteOverride bool // Allow duplicate route registration
}

// defaultErrorHandler is the default error handler,
// which returns 500 Internal Server Error.
func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// defaultShutdownHandler is the default shutdown handler,
// which returns 503 Service Unavailable.
func defaultShutdownHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Retry-After", "60") // Recommend retrying after 60 seconds
	http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
}

// defaultTimeoutHandler is the default timeout handler,
// which returns 503 Service Unavailable.
func defaultTimeoutHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Retry-After", "60") // Recommend retrying after 60 seconds
	http.Error(w, "Request processing timed out", http.StatusServiceUnavailable)
}

// NewRouter initializes and returns a new Router instance.
// Initializes the DoubleArrayTrie for static routes and the cache, and sets the default error handler.
func NewRouter() *Router {
	return NewRouterWithOptions(DefaultRouterOptions())
}

// NewRouterWithOptions initializes and returns a new Router instance with the specified options.
func NewRouterWithOptions(opts RouterOptions) *Router {
	// Cache size verification
	cacheMaxEntries := defaultCacheMaxEntries
	if opts.CacheMaxEntries > 0 {
		cacheMaxEntries = opts.CacheMaxEntries
	}

	// Timeout verification
	requestTimeout := 0 * time.Second // Default no timeout
	if opts.RequestTimeout >= 0 {
		requestTimeout = opts.RequestTimeout
	}

	r := &Router{
		staticTrie:         newDoubleArrayTrie(),
		cache:              NewCache(cacheMaxEntries),
		errorHandler:       defaultErrorHandler,
		shutdownHandler:    defaultShutdownHandler,
		timeoutHandler:     defaultTimeoutHandler,
		notFoundHandler:    nil,             // Default to nil, will use http.NotFound
		paramsPool:         NewParamsPool(), // Initialize parameter pool
		routes:             make([]*Route, 0),
		groups:             make([]*Group, 0),
		requestTimeout:     requestTimeout,
		allowRouteOverride: opts.AllowRouteOverride,
	}
	// Initialize middleware list (using atomic.Value)
	r.middleware.Store(make([]MiddlewareFunc, 0, 8))
	// Initialize cleanupable middleware list
	r.cleanupMws.Store(make([]CleanupMiddleware, 0, 8))
	// shuttingDown is default false but explicitly set
	r.shuttingDown.Store(false)

	// Initialize dynamic route trees for each HTTP method
	for i := range r.dynamicNodes {
		r.dynamicNodes[i] = NewNode("")
	}

	return r
}

// SetErrorHandler sets a custom error handler.
// This allows implementing application-specific error handling.
// errorHandlerはルートハンドラー内で発生したエラーを処理するための関数です。
// アプリケーションロジックのエラーに対して適切な対応を定義できます。
func (r *Router) SetErrorHandler(h func(http.ResponseWriter, *http.Request, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorHandler = h
}

// SetShutdownHandler sets a custom shutdown handler.
// This allows customizing request processing during shutdown.
// shutdownHandlerはサーバーがシャットダウン中の場合のリクエスト処理を担当します。
// サーバーの終了処理中に新しいリクエストが来た場合の特殊な対応を定義できます。
func (r *Router) SetShutdownHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shutdownHandler = h
}

// SetTimeoutHandler sets the timeout handling function.
// timeoutHandlerはリクエスト処理がタイムアウトした場合の処理を担当します。
// 処理時間が長すぎるリクエストに対する特殊な対応を定義できます。
func (r *Router) SetTimeoutHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timeoutHandler = h
}

// SetNotFoundHandler sets a custom handler for routes that are not found.
// This allows customizing the 404 Not Found response.
// notFoundHandlerは存在しないルートへのリクエストを処理します。
// ルーティングの段階で一致するルートが見つからない場合の対応を定義できます。
func (r *Router) SetNotFoundHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notFoundHandler = h
}

// Use adds one or more middleware functions to the router.
// Middleware functions are executed before all route handlers, allowing for common processing such as authentication and logging.
func (r *Router) Use(mw ...MiddlewareFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get current middleware list
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
func (r *Router) AddCleanupMiddleware(cm CleanupMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Get current middleware list
	currentMiddleware := r.middleware.Load().([]MiddlewareFunc)

	// Create new middleware list (existing + new)
	newMiddleware := make([]MiddlewareFunc, len(currentMiddleware)+1)
	copy(newMiddleware, currentMiddleware)
	newMiddleware[len(currentMiddleware)] = cm.Middleware()

	// Atomic update
	r.middleware.Store(newMiddleware)

	// Add to cleanup list
	currentCleanup := r.cleanupMws.Load().([]CleanupMiddleware)
	newCleanup := make([]CleanupMiddleware, len(currentCleanup)+1)
	copy(newCleanup, currentCleanup)
	newCleanup[len(currentCleanup)] = cm

	r.cleanupMws.Store(newCleanup)
}

// ServeHTTP handles HTTP requests.
// It performs route matching, calls the appropriate handler,
// builds the middleware chain, and handles errors.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Create a response wrapper to track write status
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

	// Declare timeout-related variables at the beginning of the function
	var cancel context.CancelFunc
	var done chan struct{}
	var timeoutOccurred atomic.Bool // Track whether a timeout occurred

	// Clean up resources even if a panic occurs
	defer func() {
		if cancel != nil {
			cancel() // Cancel the context
		}
		if done != nil {
			close(done) // Terminate the timeout monitoring goroutine
		}
	}()

	// Find handler and route
	handler, route, found := r.findHandlerAndRoute(req.Method, req.URL.Path)
	if !found {
		// 404 handling with custom handler if set
		r.mu.RLock()
		notFoundHandler := r.notFoundHandler
		r.mu.RUnlock()

		if notFoundHandler != nil {
			notFoundHandler(rw, req)
		} else {
			http.NotFound(rw, req)
		}
		return
	}

	// Set processing time limit
	ctx := req.Context()

	// Apply the configured timeout if no existing deadline
	if _, ok := ctx.Deadline(); !ok {
		// Get timeout setting (use route-specific setting if available)
		var timeout time.Duration
		if route != nil {
			timeout = route.GetTimeout()
		} else {
			r.timeoutMu.RLock()
			timeout = r.requestTimeout
			r.timeoutMu.RUnlock()
		}

		// Apply timeout only if it's set
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel() // Prevent context leak
			req = req.WithContext(ctx)

			// Monitor context cancellation
			done = make(chan struct{})

			// Timeout monitoring goroutine
			go func() {
				select {
				case <-ctx.Done():
					if ctx.Err() == context.DeadlineExceeded {
						// If timeout, call timeout handler
						timeoutOccurred.Store(true)

						// Process only if response hasn't been written yet
						if !rw.Written() {
							r.mu.RLock()
							timeoutHandler := r.timeoutHandler
							r.mu.RUnlock()
							if timeoutHandler != nil {
								timeoutHandler(rw, req)
							} else {
								// Default timeout processing
								http.Error(rw, "Request timeout", http.StatusGatewayTimeout)
							}
						}
					}
				case <-done:
					// Normal processing completed
				}
			}()
		}
	}

	// If shutting down, call shutdown handler
	// Since atomic.Bool is used, reading is synchronized
	// Copy shuttingDown flag to local variable to prevent data race
	isShuttingDown := r.shuttingDown.Load()
	if isShuttingDown {
		r.mu.RLock()
		shutdownHandler := r.shutdownHandler
		r.mu.RUnlock()
		shutdownHandler(rw, req)
		return
	}

	// Count active requests
	// sync.WaitGroup is internally synchronized,
	// but mutex is used to prevent simultaneous access from multiple goroutines
	r.wgMu.Lock()
	r.activeRequests.Add(1)
	r.wgMu.Unlock()

	defer func() {
		r.activeRequests.Done() // Call Done without mutex
	}()

	// Get URL parameters
	params, paramsFound := r.cache.GetParams(generateRouteKey(methodToUint8(req.Method), normalizePath(req.URL.Path)))
	if paramsFound && len(params) > 0 {
		// If parameters could be retrieved from cache
		ps := r.paramsPool.Get()
		for k, v := range params {
			ps.Add(k, v)
		}
		ctx = contextWithParams(ctx, ps)
		req = req.WithContext(ctx)
		defer r.paramsPool.Put(ps)
	}

	// Build middleware chain and execute
	h := r.buildMiddlewareChain(handler)
	err := h(rw, req)

	// If an error occurs, call error handler
	if err != nil {
		// If timeout has already occurred, do not process
		if timeoutOccurred.Load() {
			return
		}

		// Process only if response hasn't been written yet
		if !rw.Written() {
			// Handle panic in error handler
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Error handler panic: %v", r)
					if !rw.Written() {
						http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()

			// Use route-specific error handler if available
			var errorHandler func(http.ResponseWriter, *http.Request, error)
			if route != nil && route.errorHandler != nil {
				errorHandler = route.errorHandler
			} else {
				r.mu.RLock()
				errorHandler = r.errorHandler
				r.mu.RUnlock()
			}

			// Call error handler
			errorHandler(rw, req, err)
		}
	}
}

// buildMiddlewareChain applies all middleware to a handler function,
// building the final execution chain. Middleware is applied in the order they are registered (first registered first executed).
func (r *Router) buildMiddlewareChain(final HandlerFunc) HandlerFunc {
	middleware := r.middleware.Load().([]MiddlewareFunc)
	return applyMiddlewareChain(final, middleware)
}

// findHandlerAndRoute searches for a handler and route that matches the request path and method.
// It uses cache for fast search and falls back to static routes and dynamic routes if not in cache.
func (r *Router) findHandlerAndRoute(method, path string) (HandlerFunc, *Route, bool) {
	// Normalize path
	path = normalizePath(path)

	// Convert HTTP method to value
	methodIndex := methodToUint8(method)
	if methodIndex == 0 {
		return nil, nil, false
	}

	// Generate cache key
	key := generateRouteKey(methodIndex, path)

	// Check cache
	if handler, found := r.cache.Get(key); found {
		// Cache hit
		return handler, nil, true
	}

	// Search static route
	if handler := r.staticTrie.Search(path); handler != nil {
		// If static route is found, add to cache
		r.cache.Set(key, handler, nil)
		return handler, nil, true
	}

	// Search dynamic route
	nodeIndex := methodIndex - 1
	node := r.dynamicNodes[nodeIndex]
	if node != nil {
		// Get parameter object from pool
		params := r.paramsPool.Get()
		handler, matched := node.Match(path, params)
		if matched && handler != nil {
			// If dynamic route is found, add to cache
			// Convert parameters to map
			paramsMap := make(map[string]string, params.Len())
			for i := 0; i < params.Len(); i++ {
				key, val := params.data[i].key, params.data[i].value
				paramsMap[key] = val
			}
			r.cache.Set(key, handler, paramsMap)

			// Return parameter object to pool
			r.paramsPool.Put(params)
			return handler, nil, true
		}
		// Return parameter object to pool
		r.paramsPool.Put(params)
	}

	// Route not found
	return nil, nil, false
}

// Handle registers a new route. If the pattern is static, it registers in DoubleArrayTrie,
// if it contains dynamic parameters, it registers in Radix tree.
// It also validates the pattern, HTTP method, and handler function.
// If static routes and dynamic routes conflict, static routes take precedence.
// Route processing is determined by the allowRouteOverride option:
// - true: The later registered route overwrites the existing route.
// - false: If a duplicate route is detected, an error is returned (default).
func (r *Router) Handle(method, pattern string, h HandlerFunc) error {
	// Validate pattern
	if pattern == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty pattern"}
	}

	// Normalize path (add leading / and remove trailing /)
	pattern = normalizePath(pattern)

	// Validate handler and method
	if h == nil {
		return &RouterError{Code: ErrNilHandler, Message: "nil handler"}
	}
	if err := validateMethod(method); err != nil {
		return err
	}
	if err := validatePattern(pattern); err != nil {
		return err
	}

	// Split pattern into segments and determine whether static or dynamic
	methodIndex := methodToUint8(method)
	segments := parseSegments(pattern)
	isStatic := isAllStatic(segments)

	// Duplicate check
	r.mu.Lock()
	defer r.mu.Unlock()

	// Static route case
	if isStatic {
		// Duplicate check for static route
		existingHandler := r.staticTrie.Search(pattern)
		if existingHandler != nil {
			// If duplicate is found
			if !r.allowRouteOverride {
				return &RouterError{Code: ErrInvalidPattern, Message: "duplicate static route: " + pattern}
			}
			// If overwrite mode, overwrite existing route
			return r.staticTrie.Add(pattern, h)
		}

		// Dynamic route and static route conflict check
		nodeIndex := methodIndex - 1
		node := r.dynamicNodes[nodeIndex]
		if node != nil {
			params := NewParams()
			existingHandler, matched := node.Match(pattern, params)
			PutParams(params) // Return parameter object to pool

			// If dynamic route already exists
			if matched && existingHandler != nil {
				if !r.allowRouteOverride {
					return &RouterError{Code: ErrInvalidPattern, Message: "route already registered as dynamic route: " + pattern}
				}
				// If overwrite mode, prioritize static route (overwrite dynamic route)
			}
		}

		// Register new static route
		return r.staticTrie.Add(pattern, h)
	}

	// Dynamic route case
	// Static route and dynamic route conflict check
	existingHandler := r.staticTrie.Search(pattern)
	if existingHandler != nil {
		// If static route already exists
		if !r.allowRouteOverride {
			return &RouterError{Code: ErrInvalidPattern, Message: "route already registered as static route: " + pattern}
		}
		// If overwrite mode, prioritize static route (return error)
		return &RouterError{Code: ErrInvalidPattern, Message: "cannot override static route with dynamic route: " + pattern}
	}

	// Register dynamic route
	nodeIndex := methodIndex - 1
	node := r.dynamicNodes[nodeIndex]
	if node == nil {
		// Initialize dynamic route tree for this HTTP method
		node = NewNode("")
		r.dynamicNodes[nodeIndex] = node
	}

	// Check existing dynamic route
	if r.allowRouteOverride {
		// If overwrite mode, remove existing route before adding
		node.RemoveRoute(segments)
	}

	// Add route
	if err := node.AddRoute(segments, h); err != nil {
		return err
	}

	return nil
}

// parseSegments splits the URL path into an array of segments separated by "/".
// Leading "/" is removed, and if the path is empty or just "/", it returns an array containing an empty string.
func parseSegments(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}
	if path[0] == '/' {
		path = path[1:]
	}
	return strings.Split(path, "/")
}

// isAllStatic determines whether the array of segments is all static (no parameters).
// If there is even one dynamic segment (e.g., {param} format), it returns false.
func isAllStatic(segs []string) bool {
	return !slices.ContainsFunc(segs, isDynamicSeg)
}

// isDynamicSeg determines whether a segment is a dynamic parameter (e.g., {param} format).
// If the segment starts with "{" and ends with "}", it is considered a dynamic segment.
func isDynamicSeg(seg string) bool {
	if seg == "" {
		return false
	}
	return seg[0] == '{' && seg[len(seg)-1] == '}'
}

// generateRouteKey generates a cache key from HTTP method and path.
// It uses FNV-1a hashing algorithm for fast unique key generation.
func generateRouteKey(method uint8, path string) uint64 {
	// FNV-1a hashing constants
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)

	// Initialize hash value
	hash := offset64

	// Incorporate method into hash
	hash ^= uint64(method)
	hash *= prime64

	// Incorporate each byte of path into hash (directly access string without converting to byte slice)
	for i := 0; i < len(path); i++ {
		hash ^= uint64(path[i])
		hash *= prime64
	}

	return hash
}

// methodToUint8 converts the HTTP method string to its internal numeric representation.
// It assigns values 1-7 to each method and returns 0 for unsupported methods.
// This value is used as the index in the dynamicNodes array.
func methodToUint8(m string) uint8 {
	switch m {
	case http.MethodGet:
		return 1
	case http.MethodPost:
		return 2
	case http.MethodPut:
		return 3
	case http.MethodDelete:
		return 4
	case http.MethodPatch:
		return 5
	case http.MethodHead:
		return 6
	case http.MethodOptions:
		return 7
	default:
		return 0
	}
}

// contextWithParams adds URL parameters to the request context.
// This allows accessing parameters in handler functions using GetParams(r.Context()).
func contextWithParams(ctx context.Context, ps *Params) context.Context {
	return context.WithValue(ctx, paramsKey{}, ps)
}

// Shutdown gracefully shuts down the router.
// It stops accepting new requests and waits for existing requests to complete.
// If the specified context is canceled, it stops waiting and returns an error.
func (r *Router) Shutdown(ctx context.Context) error {
	// Set shuttingDown flag
	r.shuttingDown.Store(true)

	// Stop cache cleanup loop
	r.cache.Stop()

	// Clean up cleanupable middleware
	cleanupMws := r.cleanupMws.Load().([]CleanupMiddleware)
	for _, cm := range cleanupMws {
		if err := cm.Cleanup(); err != nil {
			return err
		}
	}

	// Wait for active requests to complete
	waitCh := make(chan struct{})
	go func() {
		r.activeRequests.Wait()
		close(waitCh)
	}()

	// Wait for context cancellation or all requests to complete
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
		return nil
	}
}

// ShutdownWithTimeoutContext gracefully shuts down the router with a timeout.
// It returns an error if all requests do not complete within the specified time.
func (r *Router) ShutdownWithTimeoutContext(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.Shutdown(ctx)
}

// MustHandle is the panicking version of Handle.
// If an error occurs, it panics.
func (r *Router) MustHandle(method, pattern string, h HandlerFunc) {
	if err := r.Handle(method, pattern, h); err != nil {
		panic(err)
	}
}

// Route registers a new route. If the pattern is static, it registers in DoubleArrayTrie,
// if it contains dynamic parameters, it registers in Radix tree.
// It also validates the pattern, HTTP method, and handler function.
// If static routes and dynamic routes conflict, static routes take precedence.
// Other duplicate patterns (e.g., duplicate registration of the same path) are errors.
func (r *Router) Route(method, pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	// Normalize path
	pattern = normalizePath(pattern)

	route := &Route{
		group:        nil, // Directly registered routes do not belong to a group
		router:       r,   // Set reference to router
		method:       method,
		subPath:      pattern,
		handler:      h,
		middleware:   make([]MiddlewareFunc, 0, len(middleware)),
		applied:      false,
		timeout:      0,
		errorHandler: nil, // Set to nil (use default value of router)
	}

	// Add middleware
	if len(middleware) > 0 {
		route.middleware = append(route.middleware, middleware...)
	}

	// Add route to router
	r.routes = append(r.routes, route)

	return route
}

// Get creates a route for the GET method.
// The returned Route object can be used to apply specific middleware.
func (r *Router) Get(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodGet, pattern, h, middleware...)
}

// Post creates a route for the POST method.
func (r *Router) Post(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPost, pattern, h, middleware...)
}

// Put creates a route for the PUT method.
func (r *Router) Put(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPut, pattern, h, middleware...)
}

// Delete creates a route for the DELETE method.
func (r *Router) Delete(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodDelete, pattern, h, middleware...)
}

// Patch creates a route for the PATCH method.
func (r *Router) Patch(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPatch, pattern, h, middleware...)
}

// Head creates a route for the HEAD method.
func (r *Router) Head(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodHead, pattern, h, middleware...)
}

// Options creates a route for the OPTIONS method.
func (r *Router) Options(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodOptions, pattern, h, middleware...)
}

// Build registers all routes.
// This method must be explicitly called.
// Route processing is determined by the router's allowRouteOverride option:
// - true: The later registered route overwrites the existing route.
// - false: If a duplicate route is detected, an error is returned (default).
func (r *Router) Build() error {
	// Global duplicate check map
	globalRouteMap := make(map[string]string)

	// Temporarily save directly registered routes
	directRoutes := make([]*Route, len(r.routes))
	copy(directRoutes, r.routes)

	// Collect routes for groups
	var allGroupRoutes []*Route
	for i, group := range r.groups {
		groupID := "group" + strconv.Itoa(i)
		groupRoutes, err := r.collectGroupRoutes(group, globalRouteMap, groupID)
		if err != nil && !r.allowRouteOverride {
			return err
		}
		allGroupRoutes = append(allGroupRoutes, groupRoutes...)
	}

	// Pre-check all routes (check for duplicates and invalid patterns)
	for _, route := range directRoutes {
		// Generate route information in advance
		routeKey := route.method + ":" + route.subPath

		// Duplicate check
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			if !r.allowRouteOverride {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "duplicate route definition: " + route.method + " " + route.subPath + " (conflicts with " + existingRoute + ")",
				}
			}
			// If overwrite mode, output warning
			log.Printf("Warning: overriding route: %s %s (previously defined as %s)",
				route.method, route.subPath, existingRoute)
		}

		// Add route information to map
		routeInfo := "router:" + route.method + " " + route.subPath
		globalRouteMap[routeKey] = routeInfo

		// Apply middleware to handler
		var handler HandlerFunc
		if len(route.middleware) > 0 {
			handler = applyMiddlewareChain(route.handler, route.middleware)
		} else {
			handler = route.handler
		}

		// Route validation (actually not registered)
		if err := r.validateRoute(route.method, route.subPath, handler); err != nil {
			return err
		}
	}

	// Pre-check routes for groups
	for _, route := range allGroupRoutes {
		// Calculate full path
		var fullPath string
		if route.group != nil {
			fullPath = joinPath(route.group.prefix, normalizePath(route.subPath))
		} else {
			fullPath = route.subPath
		}

		// Generate route information in advance
		routeKey := route.method + ":" + fullPath

		// Duplicate check
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			if !r.allowRouteOverride {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "duplicate route definition: " + route.method + " " + fullPath + " (conflicts with " + existingRoute + ")",
				}
			}
			// If overwrite mode, output warning
			log.Printf("Warning: overriding route: %s %s (previously defined as %s)",
				route.method, fullPath, existingRoute)
		}

		// Add route information to map
		routeInfo := "group:" + route.method + " " + fullPath
		globalRouteMap[routeKey] = routeInfo

		// Apply middleware to handler
		var handler HandlerFunc
		if len(route.middleware) > 0 {
			handler = applyMiddlewareChain(route.handler, route.middleware)
		} else {
			handler = route.handler
		}

		// Route validation (actually not registered)
		if err := r.validateRoute(route.method, fullPath, handler); err != nil {
			return err
		}
	}

	// If all checks pass, actually register
	for _, route := range directRoutes {
		if err := route.build(); err != nil && !r.allowRouteOverride {
			return err
		}
	}

	for _, route := range allGroupRoutes {
		if err := route.build(); err != nil && !r.allowRouteOverride {
			return err
		}
	}

	return nil
}

// validateRoute checks the route but does not actually register it.
// It is only for validation in the Handle method.
func (r *Router) validateRoute(method, pattern string, h HandlerFunc) error {
	// Path validation
	if pattern == "" || (len(pattern) > 1 && pattern[0] != '/') {
		return &RouterError{Code: ErrInvalidPattern, Message: "invalid path: " + pattern}
	}

	// Convert HTTP method to value
	methodIndex := methodToUint8(method)
	if methodIndex == 0 {
		return &RouterError{Code: ErrInvalidMethod, Message: "unsupported HTTP method: " + method}
	}

	// Handler function validation
	if h == nil {
		return &RouterError{Code: ErrNilHandler, Message: "handler function is nil"}
	}

	return nil
}

// collectGroupRoutes collects all routes in a group and performs global duplicate check.
func (r *Router) collectGroupRoutes(group *Group, globalRouteMap map[string]string, groupID string) ([]*Route, error) {
	var routes []*Route

	// Collect routes in group
	for _, route := range group.routes {
		if route.applied {
			continue
		}

		// Calculate full path
		fullPath := joinPath(group.prefix, normalizePath(route.subPath))
		routeKey := route.method + ":" + fullPath

		// Global duplicate check
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			return nil, &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate route definition: " + route.method + " " + fullPath + " (conflicts with " + existingRoute + ")",
			}
		}
		globalRouteMap[routeKey] = groupID + ":" + route.method + " " + fullPath

		routes = append(routes, route)
	}

	return routes, nil
}

// SetRequestTimeout sets the request processing timeout time.
// A value of 0 or less disables the timeout.
func (r *Router) SetRequestTimeout(timeout time.Duration) {
	r.timeoutMu.Lock()
	defer r.timeoutMu.Unlock()
	r.requestTimeout = timeout
}

// GetRequestTimeout returns the currently set request processing timeout time.
func (r *Router) GetRequestTimeout() time.Duration {
	r.timeoutMu.RLock()
	defer r.timeoutMu.RUnlock()
	return r.requestTimeout
}

// TimeoutSettings returns the timeout settings for the router, group, and route as a string.
// It shows the inheritance relationship and override status.
func (r *Router) TimeoutSettings() string {
	var result strings.Builder

	// Router level setting
	result.WriteString("Router Default Timeout: " + r.GetRequestTimeout().String() + "\n")

	// Direct routes setting
	if len(r.routes) > 0 {
		result.WriteString("Direct Routes:\n")
		for _, route := range r.routes {
			timeoutSource := "inherited"
			if route.timeout > 0 {
				timeoutSource = "override"
			}
			routeInfo := "  " + route.method + " " + route.subPath + ": Timeout=" +
				route.GetTimeout().String() + " (" + timeoutSource + ")\n"
			result.WriteString(routeInfo)
		}
	}

	// Group and its routes setting
	if len(r.groups) > 0 {
		result.WriteString("Groups:\n")
		for _, group := range r.groups {
			groupInfo := buildGroupTimeoutSettings(group, 1)
			result.WriteString(groupInfo)
		}
	}

	return result.String()
}

// buildGroupTimeoutSettings returns the timeout settings for a group and its routes as a string.
func buildGroupTimeoutSettings(group *Group, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("  ", indent)

	// Group setting
	timeoutSource := "inherited"
	if group.timeout > 0 {
		timeoutSource = "override"
	}

	groupInfo := indentStr + "Group '" + group.prefix + "': Timeout=" +
		group.GetTimeout().String() + " (" + timeoutSource + ")\n"
	result.WriteString(groupInfo)

	// Route setting
	for _, route := range group.routes {
		routeInfo := buildRouteTimeoutSettings(route, indent+1)
		result.WriteString(routeInfo)
	}

	return result.String()
}

// buildRouteTimeoutSettings returns the timeout settings for a route as a string.
func buildRouteTimeoutSettings(route *Route, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	// Route setting
	timeoutSource := "inherited"
	if route.timeout > 0 {
		timeoutSource = "override"
	}

	return indentStr + "Route '" + route.method + " " + route.subPath + "': Timeout=" +
		route.GetTimeout().String() + " (" + timeoutSource + ")\n"
}

// GetErrorHandler returns the default error handler for the router.
// If no error handler is set, it returns the default error handler.
func (r *Router) GetErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.errorHandler != nil {
		return r.errorHandler
	}
	return defaultErrorHandler
}

// ErrorHandlerSettings returns the error handler settings for the router, group, and route as a string.
// It shows the inheritance relationship and override status.
func (r *Router) ErrorHandlerSettings() string {
	var result strings.Builder

	// Router level setting
	result.WriteString("Router Default Error Handler: " + handlerToString(r.GetErrorHandler()) + "\n")

	// Direct routes setting
	if len(r.routes) > 0 {
		result.WriteString("Direct Routes:\n")
		for _, route := range r.routes {
			handlerSource := "inherited"
			if route.errorHandler != nil {
				handlerSource = "override"
			}
			routeInfo := "  " + route.method + " " + route.subPath + ": Error Handler=" +
				handlerToString(route.GetErrorHandler()) + " (" + handlerSource + ")\n"
			result.WriteString(routeInfo)
		}
	}

	// Groups and their routes setting
	if len(r.groups) > 0 {
		result.WriteString("Groups:\n")
		for _, group := range r.groups {
			groupInfo := buildGroupErrorHandlerSettings(group, 1)
			result.WriteString(groupInfo)
		}
	}

	return result.String()
}

// handlerToString converts a handler function to its string representation
func handlerToString(handler any) string {
	if handler == nil {
		return "nil"
	}
	return reflect.TypeOf(handler).String()
}

// buildGroupErrorHandlerSettings returns the error handler settings for a group and its routes as a string
func buildGroupErrorHandlerSettings(group *Group, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("  ", indent)

	// Group settings
	handlerSource := "inherited"
	if group.errorHandler != nil {
		handlerSource = "override"
	}

	groupInfo := indentStr + "Group '" + group.prefix + "': Error Handler=" +
		handlerToString(group.GetErrorHandler()) + " (" + handlerSource + ")\n"
	result.WriteString(groupInfo)

	// Route settings
	for _, route := range group.routes {
		routeInfo := buildRouteErrorHandlerSettings(route, indent+1)
		result.WriteString(routeInfo)
	}

	return result.String()
}

// buildRouteErrorHandlerSettings returns the error handler settings for a route as a string
func buildRouteErrorHandlerSettings(route *Route, indent int) string {
	indentStr := strings.Repeat("  ", indent)
	handlerSource := "default"

	if route.errorHandler != nil {
		handlerSource = "override"
	}

	return indentStr + "Route '" + route.method + " " + route.subPath + "': Error Handler=" +
		handlerToString(route.GetErrorHandler()) + " (" + handlerSource + ")\n"
}
