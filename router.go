package router

import (
	"context"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// HandlerFunc はエラーを返すハンドラ型です。
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// MiddlewareFunc はミドルウェアチェーンで HandlerFunc をラップする型です。
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// Router は静的ルートはダブルトライ、動的ルートは Radix ツリーで管理します。
type Router struct {
	staticTrie   *DoubleArrayTrie // 静的ルート用
	dynamicNodes [8]*Node         // メソッドごとの動的ルート (Radix ツリー) ※ GET=1, POST=2, … (index = methodToUint8 - 1)
	errHandler   func(http.ResponseWriter, *http.Request, error)
	cache        *Cache       // キャッシュ
	mw           atomic.Value // []MiddlewareFunc
	mu           sync.RWMutex
}

// NewRouter は新しい Router を生成します。
func NewRouter() *Router {
	r := &Router{
		staticTrie: newDoubleArrayTrie(),
		cache:      newCache(),
		errHandler: defaultErrorHandler,
	}
	r.mw.Store(make([]MiddlewareFunc, 0, 8))
	// dynamicNodes は初期状態では nil（登録時に作成）
	return r
}

func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	log.Printf("Unhandled error: %v", err)
	http.Error(w, "Internal Server Error", http.StatusInternalServerError)
}

// SetErrorHandler はエラー時の共通ハンドラを設定します。
func (r *Router) SetErrorHandler(h func(http.ResponseWriter, *http.Request, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errHandler = h
}

// Use はミドルウェアチェーンを追加します。
func (r *Router) Use(mw ...MiddlewareFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	curr := r.mw.Load().([]MiddlewareFunc)
	newList := make([]MiddlewareFunc, len(curr)+len(mw))
	copy(newList, curr)
	copy(newList[len(curr):], mw)
	r.mw.Store(newList)
}

func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// リクエストパスも正規化
	path := normalizePath(req.URL.Path)
	handler, ok := r.match(req.Method, path)
	if !ok {
		http.NotFound(w, req)
		return
	}
	final := r.buildChain(handler)
	if err := final(w, req); err != nil {
		r.mu.RLock()
		eh := r.errHandler
		r.mu.RUnlock()
		eh(w, req, err)
	}
}

// buildChain は登録されたミドルウェアでハンドラをラップします。
func (r *Router) buildChain(final HandlerFunc) HandlerFunc {
	m := r.mw.Load().([]MiddlewareFunc)
	for i := len(m) - 1; i >= 0; i-- {
		final = m[i](final)
	}
	return final
}

// match は、キャッシュ、静的ルート、動的ルートの順でマッチングを行います。
func (r *Router) match(method, path string) (HandlerFunc, bool) {
	meth := methodToUint8(method)
	if meth == 0 {
		return nil, false
	}
	key := generateRouteKey(meth, path)
	// キャッシュチェック
	if h, ok := r.cache.Get(key); ok {
		return h, true
	}
	// 静的ルート（ダブルトライ）
	if h := r.staticTrie.Search(path); h != nil {
		r.cache.Set(key, h)
		return h, true
	}
	// 動的ルート（Radix ツリー）
	index := meth - 1
	n := r.dynamicNodes[index]
	if n == nil {
		return nil, false
	}
	ps := NewParams()
	handler, matched := n.Match(path, ps)
	if matched && handler != nil {
		r.cache.Set(key, handler)
		return wrapWithParams(handler, ps), true
	}
	return nil, false
}

func wrapWithParams(h HandlerFunc, ps *Params) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		ctx := contextWithParams(r.Context(), ps)
		r = r.WithContext(ctx)
		defer PutParams(ps)
		return h(w, r)
	}
}

// Handle はルート登録。静的なら Trie、動的なら Radix ツリーに登録します。
func (r *Router) Handle(method, pattern string, h HandlerFunc) error {
	if pattern == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty pattern"}
	}
	pattern = normalizePath(pattern)
	if h == nil {
		return &RouterError{Code: ErrNilHandler, Message: "nil handler"}
	}
	if err := validateMethod(method); err != nil {
		return err
	}
	if err := validatePattern(pattern); err != nil {
		return err
	}
	// 以下、ルート登録処理...
	meth := methodToUint8(method)
	segs := parseSegments(pattern)
	if isAllStatic(segs) {
		return r.staticTrie.Add(pattern, h)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	index := meth - 1
	n := r.dynamicNodes[index]
	if n == nil {
		n = NewNode("")
		r.dynamicNodes[index] = n
	}
	if err := n.AddRoute(segs, h); err != nil {
		return err
	}
	return nil
}

// ショートカット
func (r *Router) Get(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodGet, pattern, h)
}
func (r *Router) Post(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodPost, pattern, h)
}
func (r *Router) Put(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodPut, pattern, h)
}
func (r *Router) Delete(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodDelete, pattern, h)
}
func (r *Router) Patch(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodPatch, pattern, h)
}
func (r *Router) Head(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodHead, pattern, h)
}
func (r *Router) Options(pattern string, h HandlerFunc) error {
	return r.Handle(http.MethodOptions, pattern, h)
}

// --- 補助関数 ---
func parseSegments(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}
	if path[0] == '/' {
		path = path[1:]
	}
	return strings.Split(path, "/")
}

func isAllStatic(segs []string) bool {
	for _, s := range segs {
		if isDynamicSeg(s) {
			return false
		}
	}
	return true
}

func isDynamicSeg(seg string) bool {
	if seg == "" {
		return false
	}
	if seg[0] == '{' && seg[len(seg)-1] == '}' {
		return true
	}
	return false
}

// generateRouteKey は FNV-1a によるキャッシュキー生成
func generateRouteKey(method uint8, path string) uint64 {
	const prime64 = 1099511628211
	var h uint64 = 1469598103934665603
	h ^= uint64(method)
	h *= prime64
	for i := 0; i < len(path); i++ {
		h ^= uint64(path[i])
		h *= prime64
	}
	return h
}

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

func contextWithParams(ctx context.Context, ps *Params) context.Context {
	return context.WithValue(ctx, paramsKey{}, ps)
}
