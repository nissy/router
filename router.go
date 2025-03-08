package router

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HandlerFunc はHTTPリクエストを処理し、エラーを返す関数型です。
// 標準のhttp.HandlerFuncとは異なり、エラーを返すことでエラーハンドリングを統一できます。
type HandlerFunc func(http.ResponseWriter, *http.Request) error

// MiddlewareFunc はハンドラ関数を受け取り、新しいハンドラ関数を返す関数型です。
// リクエスト処理の前後に共通処理を挿入するために使用されます。
type MiddlewareFunc func(HandlerFunc) HandlerFunc

// CleanupMiddleware はクリーンアップ可能なミドルウェアのインターフェースです。
type CleanupMiddleware interface {
	Cleanup() error
	Middleware() MiddlewareFunc
}

// cleanupMiddlewareImpl はCleanupMiddlewareインターフェースの実装です。
type cleanupMiddlewareImpl struct {
	mw      MiddlewareFunc
	cleanup func() error
}

// Cleanup はCleanupMiddlewareインターフェースを実装します。
func (c *cleanupMiddlewareImpl) Cleanup() error {
	if c.cleanup != nil {
		return c.cleanup()
	}
	return nil
}

// Middleware はCleanupMiddlewareインターフェースを実装します。
func (c *cleanupMiddlewareImpl) Middleware() MiddlewareFunc {
	return c.mw
}

// NewCleanupMiddleware は新しいCleanupMiddlewareを作成します。
func NewCleanupMiddleware(mw MiddlewareFunc, cleanup func() error) CleanupMiddleware {
	return &cleanupMiddlewareImpl{
		mw:      mw,
		cleanup: cleanup,
	}
}

// Router はHTTPリクエストルーティングを管理する主要な構造体です。
// 静的ルート（DoubleArrayTrie）と動的ルート（Radixツリー）の両方をサポートし、
// 高速なルートマッチングとキャッシュ機構を提供します。
type Router struct {
	// ルーティング関連
	staticTrie   *DoubleArrayTrie // 静的ルート用の高速なトライ木構造
	dynamicNodes [8]*Node         // HTTPメソッドごとの動的ルート用Radixツリー（インデックスはmethodToUint8に対応）
	cache        *Cache           // ルートマッチングの結果をキャッシュし、パフォーマンスを向上
	routes       []*Route         // 直接登録されたルート
	groups       []*Group         // 登録されたグループ

	// ハンドラ関連
	errorHandler    func(http.ResponseWriter, *http.Request, error) // エラー発生時の処理関数
	shutdownHandler http.HandlerFunc                                // シャットダウン中のリクエスト処理関数

	// ミドルウェア関連
	middleware atomic.Value // ミドルウェア関数のリスト（スレッドセーフな更新のためatomic.Value使用）
	cleanupMws atomic.Value // クリーンアップ可能なミドルウェアのリスト

	// 同期関連
	mu             sync.RWMutex   // 並行アクセスからの保護用ミューテックス
	activeRequests sync.WaitGroup // アクティブなリクエストの数を追跡
	wgMu           sync.Mutex     // activeRequestsへのアクセスを保護するミューテックス
	shuttingDown   atomic.Bool    // シャットダウン中かどうかを示すフラグ
}

// defaultErrorHandler はデフォルトのエラーハンドラで、
// 内部サーバーエラー（500）を返します。
func defaultErrorHandler(w http.ResponseWriter, r *http.Request, err error) {
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

// defaultShutdownHandler はデフォルトのシャットダウンハンドラで、
// 503 Service Unavailableを返します。
func defaultShutdownHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Retry-After", "60") // 60秒後に再試行を推奨
	http.Error(w, "Server is shutting down", http.StatusServiceUnavailable)
}

// NewRouter は新しいRouterインスタンスを初期化して返します。
// 静的ルート用のDoubleArrayTrieとキャッシュを初期化し、デフォルトのエラーハンドラを設定します。
func NewRouter() *Router {
	r := &Router{
		staticTrie:      newDoubleArrayTrie(),
		cache:           newCache(),
		errorHandler:    defaultErrorHandler,
		shutdownHandler: defaultShutdownHandler,
		routes:          make([]*Route, 0),
		groups:          make([]*Group, 0),
	}
	// ミドルウェアリストを初期化（atomic.Valueを使用するため）
	r.middleware.Store(make([]MiddlewareFunc, 0, 8))
	// クリーンアップ可能なミドルウェアリストを初期化
	r.cleanupMws.Store(make([]CleanupMiddleware, 0, 8))
	// shuttingDownはデフォルトでfalseだが、明示的に設定
	r.shuttingDown.Store(false)

	// 各HTTPメソッド用の動的ルートツリーを初期化
	for i := range r.dynamicNodes {
		r.dynamicNodes[i] = NewNode("")
	}

	return r
}

// SetErrorHandler はカスタムエラーハンドラを設定します。
// これにより、アプリケーション固有のエラー処理を実装できます。
func (r *Router) SetErrorHandler(h func(http.ResponseWriter, *http.Request, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.errorHandler = h
}

// SetShutdownHandler はカスタムシャットダウンハンドラを設定します。
// これにより、シャットダウン中のリクエスト処理をカスタマイズできます。
func (r *Router) SetShutdownHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shutdownHandler = h
}

// Use は1つ以上のミドルウェア関数をルーターに追加します。
// ミドルウェアはすべてのルートハンドラの前に実行され、認証やロギングなどの
// 共通処理を実装するために使用できます。
func (r *Router) Use(mw ...MiddlewareFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 現在のミドルウェアリストを取得
	currentMiddleware := r.middleware.Load().([]MiddlewareFunc)

	// 新しいミドルウェアリストを作成（既存 + 新規）
	newMiddleware := make([]MiddlewareFunc, len(currentMiddleware)+len(mw))
	copy(newMiddleware, currentMiddleware)
	copy(newMiddleware[len(currentMiddleware):], mw)

	// アトミックに更新
	r.middleware.Store(newMiddleware)
}

// AddCleanupMiddleware はクリーンアップ可能なミドルウェアをルーターに追加します。
// このミドルウェアはShutdownメソッドが呼ばれたときにクリーンアップされます。
func (r *Router) AddCleanupMiddleware(cm CleanupMiddleware) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// 現在のミドルウェアリストを取得
	currentMiddleware := r.middleware.Load().([]MiddlewareFunc)

	// 新しいミドルウェアリストを作成（既存 + 新規）
	newMiddleware := make([]MiddlewareFunc, len(currentMiddleware)+1)
	copy(newMiddleware, currentMiddleware)
	newMiddleware[len(currentMiddleware)] = cm.Middleware()

	// アトミックに更新
	r.middleware.Store(newMiddleware)

	// クリーンアップリストに追加
	currentCleanup := r.cleanupMws.Load().([]CleanupMiddleware)
	newCleanup := make([]CleanupMiddleware, len(currentCleanup)+1)
	copy(newCleanup, currentCleanup)
	newCleanup[len(currentCleanup)] = cm

	r.cleanupMws.Store(newCleanup)
}

// ServeHTTP はHTTPリクエストを処理します。ルートマッチングを行い、ミドルウェアを
// 適用してハンドラを実行します。エラーが発生した場合はエラーハンドラを呼び出します。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// シャットダウン中の場合はシャットダウンハンドラを呼び出す
	// atomic.Boolを使用しているため、読み取りは同期化されている
	// シャットダウンフラグをローカル変数にコピーして、データ競合を防ぐ
	isShuttingDown := r.shuttingDown.Load()
	if isShuttingDown {
		r.mu.RLock()
		shutdownHandler := r.shutdownHandler
		r.mu.RUnlock()
		shutdownHandler(w, req)
		return
	}

	// アクティブなリクエストをカウント
	// sync.WaitGroupは内部的に同期化されているが、
	// 複数のゴルーチンからの同時アクセスを防ぐためにミューテックスで保護
	r.wgMu.Lock()
	r.activeRequests.Add(1)
	r.wgMu.Unlock()

	defer func() {
		r.activeRequests.Done() // ミューテックスなしでDoneを呼び出す
	}()

	// パスを正規化（先頭に/を追加、末尾の/を削除）
	path := normalizePath(req.URL.Path)

	// ルートマッチング
	handler, found := r.findHandler(req.Method, path)
	if !found {
		http.NotFound(w, req)
		return
	}

	// ミドルウェアチェーンを構築して実行
	finalHandler := r.buildMiddlewareChain(handler)
	if err := finalHandler(w, req); err != nil {
		// エラーハンドラを呼び出し
		r.mu.RLock()
		errorHandler := r.errorHandler
		r.mu.RUnlock()
		errorHandler(w, req, err)
	}
}

// buildMiddlewareChain はハンドラ関数にすべてのミドルウェアを適用し、
// 最終的な実行チェーンを構築します。ミドルウェアは登録された順序で
// 適用されます（最初に登録されたものが最初に実行）。
func (r *Router) buildMiddlewareChain(final HandlerFunc) HandlerFunc {
	middleware := r.middleware.Load().([]MiddlewareFunc)
	return applyMiddlewareChain(final, middleware)
}

// findHandler はHTTPメソッドとパスに一致するハンドラ関数を検索します。
// 1. キャッシュをチェック
// 2. 静的ルート（DoubleArrayTrie）をチェック
// 3. 動的ルート（Radixツリー）をチェック
// の順で検索し、最初に見つかったハンドラを返します。
func (r *Router) findHandler(method, path string) (HandlerFunc, bool) {
	// HTTPメソッドを数値に変換
	methodIndex := methodToUint8(method)
	if methodIndex == 0 {
		return nil, false // サポートされていないHTTPメソッド
	}

	// キャッシュキーを生成し、キャッシュをチェック
	cacheKey := generateRouteKey(methodIndex, path)

	if handler, paramMap, found := r.cache.GetWithParams(cacheKey); found {
		if paramMap != nil {
			// キャッシュからパラメータ情報を取得した場合
			params := NewParams()
			for k, v := range paramMap {
				params.Add(k, v)
			}
			return wrapHandlerWithParams(handler, params), true
		}
		return handler, true
	}

	// 静的ルートを検索（高速なDoubleArrayTrieを使用）
	if handler := r.staticTrie.Search(path); handler != nil {
		// キャッシュに結果を保存（静的ルートにはパラメータなし）
		r.cache.Set(cacheKey, handler, nil)
		return handler, true
	}

	// 動的ルートを検索（Radixツリーを使用）
	nodeIndex := methodIndex - 1 // 配列インデックスは0から始まるため調整
	node := r.dynamicNodes[nodeIndex]
	if node == nil {
		return nil, false // このHTTPメソッド用の動的ルートが未登録
	}

	// URLパラメータを格納するオブジェクトを取得
	params := NewParams()
	handler, matched := node.Match(path, params)

	if matched && handler != nil {
		// パラメータ情報をマップに変換
		paramMap := make(map[string]string)
		for i := 0; i < len(params.data); i++ {
			paramMap[params.data[i].key] = params.data[i].value
		}

		// キャッシュに結果を保存（パラメータ情報も含む）
		r.cache.Set(cacheKey, handler, paramMap)

		return wrapHandlerWithParams(handler, params), true
	}

	// マッチしなかった場合はパラメータをプールに返却
	PutParams(params)
	return nil, false
}

// wrapHandlerWithParams はハンドラ関数をラップし、URLパラメータをリクエストコンテキストに
// 追加します。また、パラメータオブジェクトをプールに返却するための後処理も行います。
func wrapHandlerWithParams(h HandlerFunc, ps *Params) HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) error {
		// パラメータをコンテキストに追加
		ctx := contextWithParams(r.Context(), ps)
		r = r.WithContext(ctx)

		// ハンドラ実行後にパラメータオブジェクトをプールに返却
		defer PutParams(ps)

		// 元のハンドラを実行
		return h(w, r)
	}
}

// Handle は新しいルートを登録します。パターンが静的な場合はDoubleArrayTrieに、
// 動的パラメータを含む場合はRadixツリーに登録します。
// パターン、HTTPメソッド、ハンドラ関数の検証も行います。
// 静的ルートと動的ルートが競合する場合は静的ルートが優先されます。
// その他の重複パターン（同一パスの重複登録など）はエラーとなります。
func (r *Router) Handle(method, pattern string, h HandlerFunc) error {
	// パターンの検証
	if pattern == "" {
		return &RouterError{Code: ErrInvalidPattern, Message: "empty pattern"}
	}

	// パスの正規化（先頭に/を追加、末尾の/を削除）
	pattern = normalizePath(pattern)

	// ハンドラとメソッドの検証
	if h == nil {
		return &RouterError{Code: ErrNilHandler, Message: "nil handler"}
	}
	if err := validateMethod(method); err != nil {
		return err
	}
	if err := validatePattern(pattern); err != nil {
		return err
	}

	// パターンをセグメントに分割し、静的か動的かを判断
	methodIndex := methodToUint8(method)
	segments := parseSegments(pattern)
	isStatic := isAllStatic(segments)

	// 重複チェック
	r.mu.Lock()
	defer r.mu.Unlock()

	// 静的ルートの場合
	if isStatic {
		// 静的ルートの重複チェック
		existingHandler := r.staticTrie.Search(pattern)
		if existingHandler != nil {
			return &RouterError{Code: ErrInvalidPattern, Message: "duplicate static route: " + pattern}
		}

		// 動的ルートとの競合チェック
		nodeIndex := methodIndex - 1
		node := r.dynamicNodes[nodeIndex]
		if node != nil {
			params := NewParams()
			existingHandler, matched := node.Match(pattern, params)
			PutParams(params) // パラメータオブジェクトをプールに返却

			// 動的ルートが既に存在する場合は、静的ルートを優先（エラーにしない）
			if matched && existingHandler != nil {
				// 静的ルートを登録（動的ルートを上書き）
				return r.staticTrie.Add(pattern, h)
			}
		}

		// 新しい静的ルートを登録
		return r.staticTrie.Add(pattern, h)
	}

	// 動的ルートの場合
	// 静的ルートとの競合チェック
	existingHandler := r.staticTrie.Search(pattern)
	if existingHandler != nil {
		// 静的ルートが既に存在する場合は、動的ルートを登録しない（エラーを返す）
		return &RouterError{Code: ErrInvalidPattern, Message: "route already registered as static route: " + pattern}
	}

	// 動的ルートの登録
	nodeIndex := methodIndex - 1
	node := r.dynamicNodes[nodeIndex]
	if node == nil {
		// このHTTPメソッド用の動的ルートツリーを初期化
		node = NewNode("")
		r.dynamicNodes[nodeIndex] = node
	}

	// ルートを追加
	if err := node.AddRoute(segments, h); err != nil {
		return err
	}

	return nil
}

// parseSegments はURLパスを「/」で区切ってセグメントの配列に分割します。
// 先頭の「/」は削除され、空のパスや「/」のみの場合は空の文字列を含む配列を返します。
func parseSegments(path string) []string {
	if path == "" || path == "/" {
		return []string{""}
	}
	if path[0] == '/' {
		path = path[1:]
	}
	return strings.Split(path, "/")
}

// isAllStatic はセグメントの配列がすべて静的（パラメータを含まない）かどうかを判定します。
// 一つでも動的セグメント（{param}形式）があればfalseを返します。
func isAllStatic(segs []string) bool {
	return !slices.ContainsFunc(segs, isDynamicSeg)
}

// isDynamicSeg はセグメントが動的パラメータ（{param}形式）かどうかを判定します。
// 先頭が「{」で末尾が「}」の場合は動的セグメントと判断します。
func isDynamicSeg(seg string) bool {
	if seg == "" {
		return false
	}
	return seg[0] == '{' && seg[len(seg)-1] == '}'
}

// generateRouteKey はHTTPメソッドとパスからキャッシュキーを生成します。
// FNV-1aハッシュアルゴリズムを使用して高速に一意のキーを生成します。
func generateRouteKey(method uint8, path string) uint64 {
	// FNV-1aハッシュの定数
	const (
		offset64 = uint64(14695981039346656037)
		prime64  = uint64(1099511628211)
	)

	// ハッシュ値の初期化
	hash := offset64

	// メソッドをハッシュに組み込む
	hash ^= uint64(method)
	hash *= prime64

	// パスの各バイトをハッシュに組み込む（文字列をバイトスライスに変換せずに直接アクセス）
	for i := 0; i < len(path); i++ {
		hash ^= uint64(path[i])
		hash *= prime64
	}

	return hash
}

// methodToUint8 はHTTPメソッド文字列を内部で使用する数値表現に変換します。
// 各メソッドに1〜7の値を割り当て、未対応のメソッドは0を返します。
// この数値はdynamicNodesの配列インデックスとして使用されます。
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

// contextWithParams はURLパラメータをリクエストコンテキストに追加します。
// これにより、ハンドラ関数内でGetParams(r.Context())を使ってパラメータにアクセスできます。
func contextWithParams(ctx context.Context, ps *Params) context.Context {
	return context.WithValue(ctx, paramsKey{}, ps)
}

// Shutdown はルーターをグレースフルにシャットダウンします。
// 新しいリクエストの受け付けを停止し、既存のリクエストが完了するのを待ちます。
// 指定されたコンテキストがキャンセルされた場合、待機を中止してエラーを返します。
func (r *Router) Shutdown(ctx context.Context) error {
	// シャットダウンフラグを設定
	r.shuttingDown.Store(true)

	// キャッシュを停止
	r.cache.Stop()

	// ミドルウェアのクリーンアップ
	cleanupMws := r.cleanupMws.Load().([]CleanupMiddleware)
	for _, cm := range cleanupMws {
		if err := cm.Cleanup(); err != nil {
			// エラーは無視して続行
		}
	}

	// アクティブなリクエストの完了を待機
	// ゴルーチンを使わずに直接待機することでデータ競合を防ぐ
	waitCh := make(chan struct{})
	go func() {
		// WaitGroupのWaitはロックなしで呼び出す
		r.activeRequests.Wait()
		close(waitCh)
	}()

	select {
	case <-waitCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ShutdownWithTimeoutContext はタイムアウト付きでルーターをグレースフルにシャットダウンします。
// 指定された時間内にすべてのリクエストが完了しない場合、エラーを返します。
func (r *Router) ShutdownWithTimeoutContext(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return r.Shutdown(ctx)
}

// MustHandle はHandleメソッドのパニックバージョンです。
// エラーが発生した場合はパニックを発生させます。
func (r *Router) MustHandle(method, pattern string, h HandlerFunc) {
	if err := r.Handle(method, pattern, h); err != nil {
		panic(err)
	}
}

// Route は新しいルートを作成しますが、まだ登録はしません。
// 返されたRouteオブジェクトに対してWithMiddlewareを呼び出すことで、
// 特定のミドルウェアを適用できます。
func (r *Router) Route(method, pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	// パスの正規化
	pattern = normalizePath(pattern)

	route := &Route{
		group:      nil, // ルーターに直接登録されたルートはグループに属さない
		router:     r,   // ルーターへの参照を設定
		method:     method,
		subPath:    pattern,
		handler:    h,
		middleware: make([]MiddlewareFunc, 0, len(middleware)),
		applied:    false,
	}

	// ミドルウェアを追加
	if len(middleware) > 0 {
		route.middleware = append(route.middleware, middleware...)
	}

	// ルートをルーターに追加
	r.routes = append(r.routes, route)

	return route
}

// Get はGETメソッドのルートを作成します。
// 返されたRouteオブジェクトに対してWithMiddlewareを呼び出すことで、
// 特定のミドルウェアを適用できます。
func (r *Router) Get(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodGet, pattern, h, middleware...)
}

// Post はPOSTメソッドのルートを作成します。
func (r *Router) Post(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPost, pattern, h, middleware...)
}

// Put はPUTメソッドのルートを作成します。
func (r *Router) Put(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPut, pattern, h, middleware...)
}

// Delete はDELETEメソッドのルートを作成します。
func (r *Router) Delete(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodDelete, pattern, h, middleware...)
}

// Patch はPATCHメソッドのルートを作成します。
func (r *Router) Patch(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodPatch, pattern, h, middleware...)
}

// Head はHEADメソッドのルートを作成します。
func (r *Router) Head(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodHead, pattern, h, middleware...)
}

// Options はOPTIONSメソッドのルートを作成します。
func (r *Router) Options(pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	return r.Route(http.MethodOptions, pattern, h, middleware...)
}

// Build はすべてのルートを登録します。
// このメソッドは明示的に呼び出す必要があります。
// 重複するルートが検出された場合はエラーを返します。
func (r *Router) Build() error {
	routeCount := len(r.routes)
	for _, group := range r.groups {
		routeCount += len(group.routes)
	}

	// グローバルな重複チェック用のマップ（初期容量を設定）
	globalRouteMap := make(map[string]string, routeCount)

	// 直接登録されたルートの重複チェック（初期容量を設定）
	routeMap := make(map[string]struct{}, len(r.routes))

	// 直接登録されたルートを登録
	for _, route := range r.routes {
		if route.applied {
			continue
		}

		// ルートキーを事前に生成（文字列連結を1回だけ実行）
		routeKey := route.method + ":" + route.subPath

		// ローカルな重複チェック
		if _, exists := routeMap[routeKey]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate route definition: " + route.method + " " + route.subPath,
			}
		}
		routeMap[routeKey] = struct{}{}

		// グローバルな重複チェック
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			return &RouterError{
				Code:    ErrInvalidPattern,
				Message: "duplicate route definition: " + route.method + " " + route.subPath + " (conflicts with " + existingRoute + ")",
			}
		}

		// ルート情報を事前に生成（文字列連結を1回だけ実行）
		routeInfo := "router:" + route.method + " " + route.subPath
		globalRouteMap[routeKey] = routeInfo

		// ミドルウェアをハンドラに適用
		var handler HandlerFunc
		if len(route.middleware) > 0 {
			handler = applyMiddlewareChain(route.handler, route.middleware)
		} else {
			handler = route.handler
		}

		// ルートを登録
		if err := r.Handle(route.method, route.subPath, handler); err != nil {
			return err
		}

		route.applied = true
	}

	// すべてのグループのルートを登録
	for i, group := range r.groups {
		// グループ内のルートを収集して重複チェック
		groupID := "group" + strconv.Itoa(i)
		groupRoutes, err := r.collectGroupRoutes(group, globalRouteMap, groupID)
		if err != nil {
			return err
		}

		// グループのルートを登録
		for _, route := range groupRoutes {
			if err := route.build(); err != nil {
				return err
			}
		}
	}

	return nil
}

// collectGroupRoutes はグループ内のすべてのルートを収集し、グローバルな重複チェックを行います。
func (r *Router) collectGroupRoutes(group *Group, globalRouteMap map[string]string, groupID string) ([]*Route, error) {
	var routes []*Route

	// グループ内のルートを収集
	for _, route := range group.routes {
		if route.applied {
			continue
		}

		// 完全なパスを計算
		fullPath := joinPath(group.prefix, normalizePath(route.subPath))
		routeKey := route.method + ":" + fullPath

		// グローバルな重複チェック
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
