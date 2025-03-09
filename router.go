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

// responseWriter はhttp.ResponseWriterを拡張し、レスポンスの書き込み状態を追跡します。
type responseWriter struct {
	http.ResponseWriter
	written bool
	status  int
}

// WriteHeader はHTTPステータスコードを設定します。
// 既にレスポンスが書き込まれている場合は何もしません。
func (rw *responseWriter) WriteHeader(code int) {
	if !rw.written {
		rw.status = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

// Write はレスポンスボディを書き込みます。
// 書き込みが行われると、writtenフラグが設定されます。
func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}

// Written はレスポンスが既に書き込まれているかどうかを返します。
func (rw *responseWriter) Written() bool {
	return rw.written
}

// Status は設定されたHTTPステータスコードを返します。
func (rw *responseWriter) Status() int {
	return rw.status
}

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

// RouterOptions はルーターの動作を設定するためのオプションを提供します。
type RouterOptions struct {
	// AllowRouteOverride は重複するルート登録の処理方法を指定します。
	// true: 後から登録されたルートが既存のルートを上書きします。
	// false: 重複するルートが検出された場合、エラーが返されます（デフォルト）。
	AllowRouteOverride bool

	// RequestTimeout はリクエスト処理のデフォルトタイムアウト時間です。
	// 0以下の値を指定するとタイムアウトは無効になります。
	// デフォルト: 0秒（タイムアウトなし）
	RequestTimeout time.Duration

	// CacheMaxEntries はルートキャッシュの最大エントリ数です。
	// デフォルト: 1000
	CacheMaxEntries int
}

// DefaultRouterOptions はデフォルトのルーターオプションを返します。
func DefaultRouterOptions() RouterOptions {
	return RouterOptions{
		AllowRouteOverride: false,
		RequestTimeout:     0 * time.Second, // タイムアウトなし
		CacheMaxEntries:    defaultCacheMaxEntries,
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
	timeoutHandler  http.HandlerFunc                                // タイムアウト時の処理関数

	// ミドルウェア関連
	middleware atomic.Value // ミドルウェア関数のリスト（スレッドセーフな更新のためatomic.Value使用）
	cleanupMws atomic.Value // クリーンアップ可能なミドルウェアのリスト

	// 同期関連
	mu             sync.RWMutex   // 並行アクセスからの保護用ミューテックス
	activeRequests sync.WaitGroup // アクティブなリクエストの数を追跡
	wgMu           sync.Mutex     // activeRequestsへのアクセスを保護するミューテックス
	shuttingDown   atomic.Bool    // シャットダウン中かどうかを示すフラグ

	// タイムアウト設定
	requestTimeout time.Duration // リクエスト処理のタイムアウト時間（0の場合はタイムアウトなし）
	timeoutMu      sync.RWMutex  // タイムアウト設定へのアクセスを保護するミューテックス

	// パラメータ関連
	paramsPool *ParamsPool // URLパラメータオブジェクトのプール（各ルーターインスタンス固有）

	// 設定オプション
	allowRouteOverride bool // 重複するルート登録を許可するかどうか
}

// defaultErrorHandler はデフォルトのエラーハンドラで、
// 500 Internal Server Errorを返します。
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

// defaultTimeoutHandler はデフォルトのタイムアウトハンドラで、
// 503 Service Unavailableを返します。
func defaultTimeoutHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	w.Header().Set("Retry-After", "60") // 60秒後に再試行を推奨
	http.Error(w, "Request processing timed out", http.StatusServiceUnavailable)
}

// NewRouter は新しいRouterインスタンスを初期化して返します。
// 静的ルート用のDoubleArrayTrieとキャッシュを初期化し、デフォルトのエラーハンドラを設定します。
func NewRouter() *Router {
	return NewRouterWithOptions(DefaultRouterOptions())
}

// NewRouterWithOptions は指定されたオプションで新しいRouterインスタンスを初期化して返します。
func NewRouterWithOptions(opts RouterOptions) *Router {
	// キャッシュサイズの検証
	cacheMaxEntries := defaultCacheMaxEntries
	if opts.CacheMaxEntries > 0 {
		cacheMaxEntries = opts.CacheMaxEntries
	}

	// タイムアウトの検証
	requestTimeout := 0 * time.Second // デフォルトでタイムアウトなし
	if opts.RequestTimeout >= 0 {
		requestTimeout = opts.RequestTimeout
	}

	r := &Router{
		staticTrie:         newDoubleArrayTrie(),
		cache:              NewCache(cacheMaxEntries),
		errorHandler:       defaultErrorHandler,
		shutdownHandler:    defaultShutdownHandler,
		timeoutHandler:     defaultTimeoutHandler,
		paramsPool:         NewParamsPool(), // パラメータプールを初期化
		routes:             make([]*Route, 0),
		groups:             make([]*Group, 0),
		requestTimeout:     requestTimeout,
		allowRouteOverride: opts.AllowRouteOverride,
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

// SetTimeoutHandler はタイムアウト時の処理関数を設定します。
func (r *Router) SetTimeoutHandler(h http.HandlerFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.timeoutHandler = h
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

// ServeHTTP はHTTPリクエストを処理します。
// ルートマッチングを行い、適切なハンドラを呼び出します。
// ミドルウェアチェーンを構築し、エラーハンドリングを行います。
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// レスポンスラッパーを作成して、書き込み状態を追跡
	rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

	// タイムアウト関連の変数を関数の最初で宣言
	var cancel context.CancelFunc
	var done chan struct{}
	var timeoutOccurred atomic.Bool // タイムアウトが発生したかどうかを追跡

	// パニックが発生した場合でもリソースをクリーンアップ
	defer func() {
		if cancel != nil {
			cancel() // コンテキストをキャンセル
		}
		if done != nil {
			close(done) // タイムアウト監視ゴルーチンを終了
		}
	}()

	// ハンドラとルートを検索
	handler, route, found := r.findHandlerAndRoute(req.Method, req.URL.Path)
	if !found {
		// 404処理
		http.NotFound(rw, req)
		return
	}

	// 処理時間の制限を設定
	ctx := req.Context()

	// 既存のデッドラインがない場合、設定されたタイムアウトを適用
	if _, ok := ctx.Deadline(); !ok {
		// タイムアウト設定を取得（ルート固有の設定があればそれを使用）
		var timeout time.Duration
		if route != nil {
			timeout = route.GetTimeout()
		} else {
			r.timeoutMu.RLock()
			timeout = r.requestTimeout
			r.timeoutMu.RUnlock()
		}

		// タイムアウトが設定されている場合のみ適用
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel() // コンテキストリークを防止
			req = req.WithContext(ctx)

			// コンテキストのキャンセルを監視
			done = make(chan struct{})

			// タイムアウト監視ゴルーチン
			go func() {
				select {
				case <-ctx.Done():
					if ctx.Err() == context.DeadlineExceeded {
						// タイムアウトの場合、タイムアウトハンドラを呼び出す
						timeoutOccurred.Store(true)

						// レスポンスがまだ書き込まれていない場合のみ処理
						if !rw.Written() {
							r.mu.RLock()
							timeoutHandler := r.timeoutHandler
							r.mu.RUnlock()
							if timeoutHandler != nil {
								timeoutHandler(rw, req)
							} else {
								// デフォルトのタイムアウト処理
								http.Error(rw, "Request timeout", http.StatusGatewayTimeout)
							}
						}
					}
				case <-done:
					// 正常に処理が完了
				}
			}()
		}
	}

	// シャットダウン中の場合はシャットダウンハンドラを呼び出す
	// atomic.Boolを使用しているため、読み取りは同期化されている
	// シャットダウンフラグをローカル変数にコピーして、データ競合を防ぐ
	isShuttingDown := r.shuttingDown.Load()
	if isShuttingDown {
		r.mu.RLock()
		shutdownHandler := r.shutdownHandler
		r.mu.RUnlock()
		shutdownHandler(rw, req)
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

	// URLパラメータを取得
	params, paramsFound := r.cache.GetParams(generateRouteKey(methodToUint8(req.Method), normalizePath(req.URL.Path)))
	if paramsFound && len(params) > 0 {
		// キャッシュからパラメータを取得できた場合
		ps := r.paramsPool.Get()
		for k, v := range params {
			ps.Add(k, v)
		}
		ctx = contextWithParams(ctx, ps)
		req = req.WithContext(ctx)
		defer r.paramsPool.Put(ps)
	}

	// ミドルウェアチェーンを構築して実行
	h := r.buildMiddlewareChain(handler)
	err := h(rw, req)

	// エラーが発生した場合はエラーハンドラを呼び出す
	if err != nil {
		// タイムアウトが既に発生している場合は処理しない
		if timeoutOccurred.Load() {
			return
		}

		// レスポンスがまだ書き込まれていない場合のみ処理
		if !rw.Written() {
			// エラーハンドラ内でのパニックを処理
			defer func() {
				if r := recover(); r != nil {
					log.Printf("Error handler panic: %v", r)
					if !rw.Written() {
						http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
					}
				}
			}()

			// ルート固有のエラーハンドラがあれば使用
			var errorHandler func(http.ResponseWriter, *http.Request, error)
			if route != nil && route.errorHandler != nil {
				errorHandler = route.errorHandler
			} else {
				r.mu.RLock()
				errorHandler = r.errorHandler
				r.mu.RUnlock()
			}

			// エラーハンドラを呼び出す
			errorHandler(rw, req, err)
		}
	}
}

// buildMiddlewareChain はハンドラ関数にすべてのミドルウェアを適用し、
// 最終的な実行チェーンを構築します。ミドルウェアは登録された順序で
// 適用されます（最初に登録されたものが最初に実行）。
func (r *Router) buildMiddlewareChain(final HandlerFunc) HandlerFunc {
	middleware := r.middleware.Load().([]MiddlewareFunc)
	return applyMiddlewareChain(final, middleware)
}

// findHandlerAndRoute はリクエストのパスとメソッドに一致するハンドラとルートを検索します。
// キャッシュを使用して高速に検索し、キャッシュにない場合は静的ルートと動的ルートを順に検索します。
func (r *Router) findHandlerAndRoute(method, path string) (HandlerFunc, *Route, bool) {
	// パスの正規化
	path = normalizePath(path)

	// HTTPメソッドを数値に変換
	methodIndex := methodToUint8(method)
	if methodIndex == 0 {
		return nil, nil, false
	}

	// キャッシュキーを生成
	key := generateRouteKey(methodIndex, path)

	// キャッシュを確認
	if handler, found := r.cache.Get(key); found {
		// キャッシュヒット
		return handler, nil, true
	}

	// 静的ルートを検索
	if handler := r.staticTrie.Search(path); handler != nil {
		// 静的ルートが見つかった場合はキャッシュに追加
		r.cache.Set(key, handler, nil)
		return handler, nil, true
	}

	// 動的ルートを検索
	nodeIndex := methodIndex - 1
	node := r.dynamicNodes[nodeIndex]
	if node != nil {
		// パラメータオブジェクトをプールから取得
		params := r.paramsPool.Get()
		handler, matched := node.Match(path, params)
		if matched && handler != nil {
			// 動的ルートが見つかった場合はキャッシュに追加
			// パラメータをマップに変換
			paramsMap := make(map[string]string, params.Len())
			for i := 0; i < params.Len(); i++ {
				key, val := params.data[i].key, params.data[i].value
				paramsMap[key] = val
			}
			r.cache.Set(key, handler, paramsMap)

			// パラメータオブジェクトをプールに返却
			r.paramsPool.Put(params)
			return handler, nil, true
		}
		// パラメータオブジェクトをプールに返却
		r.paramsPool.Put(params)
	}

	// ルートが見つからなかった場合
	return nil, nil, false
}

// Handle は新しいルートを登録します。パターンが静的な場合はDoubleArrayTrieに、
// 動的パラメータを含む場合はRadixツリーに登録します。
// パターン、HTTPメソッド、ハンドラ関数の検証も行います。
// 静的ルートと動的ルートが競合する場合は静的ルートが優先されます。
// 重複するルートの処理は allowRouteOverride オプションによって決まります：
// - true: 後から登録されたルートが既存のルートを上書きします。
// - false: 重複するルートが検出された場合、エラーが返されます（デフォルト）。
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
			// 重複が見つかった場合
			if !r.allowRouteOverride {
				return &RouterError{Code: ErrInvalidPattern, Message: "duplicate static route: " + pattern}
			}
			// 上書きモードの場合は、既存のルートを上書き
			return r.staticTrie.Add(pattern, h)
		}

		// 動的ルートとの競合チェック
		nodeIndex := methodIndex - 1
		node := r.dynamicNodes[nodeIndex]
		if node != nil {
			params := NewParams()
			existingHandler, matched := node.Match(pattern, params)
			PutParams(params) // パラメータオブジェクトをプールに返却

			// 動的ルートが既に存在する場合
			if matched && existingHandler != nil {
				if !r.allowRouteOverride {
					return &RouterError{Code: ErrInvalidPattern, Message: "route already registered as dynamic route: " + pattern}
				}
				// 上書きモードの場合は、静的ルートを優先（動的ルートを上書き）
			}
		}

		// 新しい静的ルートを登録
		return r.staticTrie.Add(pattern, h)
	}

	// 動的ルートの場合
	// 静的ルートとの競合チェック
	existingHandler := r.staticTrie.Search(pattern)
	if existingHandler != nil {
		// 静的ルートが既に存在する場合
		if !r.allowRouteOverride {
			return &RouterError{Code: ErrInvalidPattern, Message: "route already registered as static route: " + pattern}
		}
		// 上書きモードの場合でも、静的ルートを優先（エラーを返す）
		return &RouterError{Code: ErrInvalidPattern, Message: "cannot override static route with dynamic route: " + pattern}
	}

	// 動的ルートの登録
	nodeIndex := methodIndex - 1
	node := r.dynamicNodes[nodeIndex]
	if node == nil {
		// このHTTPメソッド用の動的ルートツリーを初期化
		node = NewNode("")
		r.dynamicNodes[nodeIndex] = node
	}

	// 既存の動的ルートをチェック
	if r.allowRouteOverride {
		// 上書きモードの場合は、既存のルートを削除してから追加
		node.RemoveRoute(segments)
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

	// キャッシュのクリーンアップループを停止
	r.cache.Stop()

	// クリーンアップ可能なミドルウェアをクリーンアップ
	cleanupMws := r.cleanupMws.Load().([]CleanupMiddleware)
	for _, cm := range cleanupMws {
		if err := cm.Cleanup(); err != nil {
			return err
		}
	}

	// アクティブなリクエストの完了を待機
	waitCh := make(chan struct{})
	go func() {
		r.activeRequests.Wait()
		close(waitCh)
	}()

	// コンテキストのキャンセルまたはすべてのリクエストの完了を待機
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-waitCh:
		return nil
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

// Route は新しいルートを登録します。パターンが静的な場合はDoubleArrayTrieに、
// 動的パラメータを含む場合はRadixツリーに登録します。
// パターン、HTTPメソッド、ハンドラ関数の検証も行います。
// 静的ルートと動的ルートが競合する場合は静的ルートが優先されます。
// その他の重複パターン（同一パスの重複登録など）はエラーとなります。
func (r *Router) Route(method, pattern string, h HandlerFunc, middleware ...MiddlewareFunc) *Route {
	// パスの正規化
	pattern = normalizePath(pattern)

	route := &Route{
		group:        nil, // ルーターに直接登録されたルートはグループに属さない
		router:       r,   // ルーターへの参照を設定
		method:       method,
		subPath:      pattern,
		handler:      h,
		middleware:   make([]MiddlewareFunc, 0, len(middleware)),
		applied:      false,
		timeout:      0,
		errorHandler: nil, // nilに設定（ルーターのデフォルト値を使用）
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
// 重複するルートの処理はルーターの allowRouteOverride オプションによって決まります：
// - true: 後から登録されたルートが既存のルートを上書きします。
// - false: 重複するルートが検出された場合、エラーが返されます（デフォルト）。
func (r *Router) Build() error {
	// グローバルな重複チェック用のマップ
	globalRouteMap := make(map[string]string)

	// 直接登録されたルートを一時的に保存
	directRoutes := make([]*Route, len(r.routes))
	copy(directRoutes, r.routes)

	// グループのルートを収集
	var allGroupRoutes []*Route
	for i, group := range r.groups {
		groupID := "group" + strconv.Itoa(i)
		groupRoutes, err := r.collectGroupRoutes(group, globalRouteMap, groupID)
		if err != nil && !r.allowRouteOverride {
			return err
		}
		allGroupRoutes = append(allGroupRoutes, groupRoutes...)
	}

	// すべてのルートを事前チェック（重複や無効なパターンをチェック）
	for _, route := range directRoutes {
		// ルート情報を事前に生成
		routeKey := route.method + ":" + route.subPath

		// 重複チェック
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			if !r.allowRouteOverride {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "duplicate route definition: " + route.method + " " + route.subPath + " (conflicts with " + existingRoute + ")",
				}
			}
			// 上書きモードの場合は警告を出力
			log.Printf("Warning: overriding route: %s %s (previously defined as %s)",
				route.method, route.subPath, existingRoute)
		}

		// ルート情報をマップに追加
		routeInfo := "router:" + route.method + " " + route.subPath
		globalRouteMap[routeKey] = routeInfo

		// ミドルウェアをハンドラに適用
		var handler HandlerFunc
		if len(route.middleware) > 0 {
			handler = applyMiddlewareChain(route.handler, route.middleware)
		} else {
			handler = route.handler
		}

		// ルートを検証（実際には登録しない）
		if err := r.validateRoute(route.method, route.subPath, handler); err != nil {
			return err
		}
	}

	// グループのルートも事前チェック
	for _, route := range allGroupRoutes {
		// 完全なパスを計算
		var fullPath string
		if route.group != nil {
			fullPath = joinPath(route.group.prefix, normalizePath(route.subPath))
		} else {
			fullPath = route.subPath
		}

		// ルート情報を事前に生成
		routeKey := route.method + ":" + fullPath

		// 重複チェック
		if existingRoute, exists := globalRouteMap[routeKey]; exists {
			if !r.allowRouteOverride {
				return &RouterError{
					Code:    ErrInvalidPattern,
					Message: "duplicate route definition: " + route.method + " " + fullPath + " (conflicts with " + existingRoute + ")",
				}
			}
			// 上書きモードの場合は警告を出力
			log.Printf("Warning: overriding route: %s %s (previously defined as %s)",
				route.method, fullPath, existingRoute)
		}

		// ルート情報をマップに追加
		routeInfo := "group:" + route.method + " " + fullPath
		globalRouteMap[routeKey] = routeInfo

		// ミドルウェアをハンドラに適用
		var handler HandlerFunc
		if len(route.middleware) > 0 {
			handler = applyMiddlewareChain(route.handler, route.middleware)
		} else {
			handler = route.handler
		}

		// ルートを検証（実際には登録しない）
		if err := r.validateRoute(route.method, fullPath, handler); err != nil {
			return err
		}
	}

	// すべてのチェックが通ったら、実際に登録
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

// validateRoute はルートを検証しますが、実際には登録しません。
// Handle メソッドの検証部分のみを抽出したものです。
func (r *Router) validateRoute(method, pattern string, h HandlerFunc) error {
	// パスの検証
	if pattern == "" || (len(pattern) > 1 && pattern[0] != '/') {
		return &RouterError{Code: ErrInvalidPattern, Message: "invalid path: " + pattern}
	}

	// HTTPメソッドを数値に変換
	methodIndex := methodToUint8(method)
	if methodIndex == 0 {
		return &RouterError{Code: ErrInvalidMethod, Message: "unsupported HTTP method: " + method}
	}

	// ハンドラ関数の検証
	if h == nil {
		return &RouterError{Code: ErrNilHandler, Message: "handler function is nil"}
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

// SetRequestTimeout はリクエスト処理のタイムアウト時間を設定します。
// 0以下の値を指定するとタイムアウトは無効になります。
func (r *Router) SetRequestTimeout(timeout time.Duration) {
	r.timeoutMu.Lock()
	defer r.timeoutMu.Unlock()
	r.requestTimeout = timeout
}

// GetRequestTimeout は現在設定されているリクエスト処理のタイムアウト時間を返します。
func (r *Router) GetRequestTimeout() time.Duration {
	r.timeoutMu.RLock()
	defer r.timeoutMu.RUnlock()
	return r.requestTimeout
}

// TimeoutSettings はルーター、グループ、ルートのタイムアウト設定を文字列として返します。
// 設定の継承関係や上書き状況を視覚的に表示します。
func (r *Router) TimeoutSettings() string {
	var result strings.Builder

	// ルーターレベルの設定
	result.WriteString("Router Default Timeout: " + r.GetRequestTimeout().String() + "\n")

	// 直接登録されたルートの設定
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

	// グループとそのルートの設定
	if len(r.groups) > 0 {
		result.WriteString("Groups:\n")
		for _, group := range r.groups {
			groupInfo := buildGroupTimeoutSettings(group, 1)
			result.WriteString(groupInfo)
		}
	}

	return result.String()
}

// buildGroupTimeoutSettings はグループとそのルートのタイムアウト設定を文字列として返します。
func buildGroupTimeoutSettings(group *Group, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("  ", indent)

	// グループの設定
	timeoutSource := "inherited"
	if group.timeout > 0 {
		timeoutSource = "override"
	}

	groupInfo := indentStr + "Group '" + group.prefix + "': Timeout=" +
		group.GetTimeout().String() + " (" + timeoutSource + ")\n"
	result.WriteString(groupInfo)

	// ルートの設定
	for _, route := range group.routes {
		routeInfo := buildRouteTimeoutSettings(route, indent+1)
		result.WriteString(routeInfo)
	}

	return result.String()
}

// buildRouteTimeoutSettings はルートのタイムアウト設定を文字列として返します。
func buildRouteTimeoutSettings(route *Route, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	// ルートの設定
	timeoutSource := "inherited"
	if route.timeout > 0 {
		timeoutSource = "override"
	}

	return indentStr + "Route '" + route.method + " " + route.subPath + "': Timeout=" +
		route.GetTimeout().String() + " (" + timeoutSource + ")\n"
}

// GetErrorHandler はルーターのデフォルトエラーハンドラを返します。
// エラーハンドラが設定されていない場合は、デフォルトのエラーハンドラを返します。
func (r *Router) GetErrorHandler() func(http.ResponseWriter, *http.Request, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.errorHandler != nil {
		return r.errorHandler
	}
	return defaultErrorHandler
}

// ErrorHandlerSettings はルーター、グループ、ルートのエラーハンドラ設定を文字列として返します。
// 設定の継承関係や上書き状況を視覚的に表示します。
func (r *Router) ErrorHandlerSettings() string {
	var result strings.Builder

	// ルーターレベルの設定
	result.WriteString("Router Default Error Handler: " + handlerToString(r.GetErrorHandler()) + "\n")

	// 直接登録されたルートの設定
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

	// グループとそのルートの設定
	if len(r.groups) > 0 {
		result.WriteString("Groups:\n")
		for _, group := range r.groups {
			groupInfo := buildGroupErrorHandlerSettings(group, 1)
			result.WriteString(groupInfo)
		}
	}

	return result.String()
}

// handlerToString はハンドラ関数を文字列表現に変換します
func handlerToString(handler interface{}) string {
	if handler == nil {
		return "nil"
	}
	return reflect.TypeOf(handler).String()
}

// buildGroupErrorHandlerSettings はグループとそのルートのエラーハンドラ設定を文字列として返します。
func buildGroupErrorHandlerSettings(group *Group, indent int) string {
	var result strings.Builder
	indentStr := strings.Repeat("  ", indent)

	// グループの設定
	handlerSource := "inherited"
	if group.errorHandler != nil {
		handlerSource = "override"
	}

	groupInfo := indentStr + "Group '" + group.prefix + "': Error Handler=" +
		handlerToString(group.GetErrorHandler()) + " (" + handlerSource + ")\n"
	result.WriteString(groupInfo)

	// ルートの設定
	for _, route := range group.routes {
		routeInfo := buildRouteErrorHandlerSettings(route, indent+1)
		result.WriteString(routeInfo)
	}

	return result.String()
}

// buildRouteErrorHandlerSettings はルートのエラーハンドラ設定を文字列として返します。
func buildRouteErrorHandlerSettings(route *Route, indent int) string {
	indentStr := strings.Repeat("  ", indent)

	// ルートの設定
	handlerSource := "inherited"
	if route.errorHandler != nil {
		handlerSource = "override"
	}

	return indentStr + "Route '" + route.method + " " + route.subPath + "': Error Handler=" +
		handlerToString(route.GetErrorHandler()) + " (" + handlerSource + ")\n"
}
