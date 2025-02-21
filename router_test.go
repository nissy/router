// router_test.go
package router_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/nissy/router"
)

func TestStaticRoute(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/static", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("static ok"))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register static route: %v", err)
	}

	req := httptest.NewRequest("GET", "/static", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "static ok" {
		t.Errorf("expected 'static ok', got %q (status %d)", body, resp.StatusCode)
	}
}

func TestDynamicRouteParam(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/users/{id}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		id, ok := ps.Get("id")
		if !ok {
			return nil
		}
		_, err := w.Write([]byte("user:" + id))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register dynamic route: %v", err)
	}

	req := httptest.NewRequest("GET", "/users/12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "user:12345" {
		t.Errorf("expected 'user:12345', got %q (status %d)", body, resp.StatusCode)
	}
}

func TestDynamicRouteRegex(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/posts/{slug:[a-z0-9-]+}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		slug, ok := ps.Get("slug")
		if !ok {
			return nil
		}
		_, err := w.Write([]byte("slug:" + slug))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register regex route: %v", err)
	}

	// マッチする例
	req := httptest.NewRequest("GET", "/posts/hello-world", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "slug:hello-world" {
		t.Errorf("expected 'slug:hello-world', got %q (status %d)", body, resp.StatusCode)
	}

	// マッチしない例（大文字を含むため）
	req = httptest.NewRequest("GET", "/posts/HelloWorld", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-matching slug, got %d", resp.StatusCode)
	}
}

func TestMultiParamRoute(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/users/{id}/posts/{postId}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		id, _ := ps.Get("id")
		postId, _ := ps.Get("postId")
		_, err := w.Write([]byte("user:" + id + " post:" + postId))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register multi-param route: %v", err)
	}

	req := httptest.NewRequest("GET", "/users/42/posts/100", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "user:42 post:100"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}
}

func TestGroupRoute(t *testing.T) {
	r := router.NewRouter()
	api := r.Group("/api")
	err := api.Get("/items/{item}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		item, _ := ps.Get("item")
		_, err := w.Write([]byte("item:" + item))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register group route: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/items/book", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "item:book"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}
}

func TestNestedGroupRoute(t *testing.T) {
	r := router.NewRouter()
	api := r.Group("/api")
	v1 := api.Group("/v1")
	err := v1.Get("/status", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("v1 status ok"))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register nested group route: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/v1/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "v1 status ok"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}
}

func TestMultipleDynamicSegments(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/a/{b}/c/{d}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		b, _ := ps.Get("b")
		d, _ := ps.Get("d")
		_, err := w.Write([]byte("b:" + b + " d:" + d))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register multi dynamic route: %v", err)
	}

	req := httptest.NewRequest("GET", "/a/alpha/c/123", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "b:alpha d:123"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}
}

func TestDynamicRouteRegexMultiple(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/api/{version:[0-9]+}/{resource:[a-z]+}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		version, _ := ps.Get("version")
		resource, _ := ps.Get("resource")
		_, err := w.Write([]byte("v:" + version + " r:" + resource))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register multi-regex route: %v", err)
	}

	// マッチする例
	req := httptest.NewRequest("GET", "/api/2/users", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "v:2 r:users"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}

	// マッチしない例：versionにアルファベット
	req = httptest.NewRequest("GET", "/api/v2/users", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404 for non-matching version, got %d", resp.StatusCode)
	}
}

func TestComplexNestedRoute(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/shop/{category}/{id:[0-9]+}/detail", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		category, _ := ps.Get("category")
		id, _ := ps.Get("id")
		_, err := w.Write([]byte("cat:" + category + " id:" + id))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register complex nested route: %v", err)
	}

	req := httptest.NewRequest("GET", "/shop/electronics/789/detail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "cat:electronics id:789"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q, got %q (status %d)", expected, body, resp.StatusCode)
	}
}

func TestTrailingSlashHandling(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/trailing/", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("trailing ok"))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register route with trailing slash: %v", err)
	}

	// 登録時は正規化されて /trailing になるので、リクエストも /trailing でマッチするはず
	req := httptest.NewRequest("GET", "/trailing", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "trailing ok" {
		t.Errorf("expected 'trailing ok', got %q (status %d)", body, resp.StatusCode)
	}
}

func TestInvalidWildcardUsage(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/files/*path", func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})
	if err == nil {
		t.Errorf("expected error for wildcard usage, but got none")
	}
}

func TestInvalidMethod(t *testing.T) {
	r := router.NewRouter()
	err := r.Handle("TRACE", "/trace", func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})
	if err == nil {
		t.Errorf("expected error for invalid method, but got none")
	}
}

func TestNilHandler(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("/nil", nil)
	if err == nil {
		t.Errorf("expected error for nil handler, but got none")
	}
}

func TestEmptyPattern(t *testing.T) {
	r := router.NewRouter()
	err := r.Get("", func(w http.ResponseWriter, r *http.Request) error {
		return nil
	})
	if err == nil {
		t.Errorf("expected error for empty pattern, but got none")
	}
}

// TestMassRegistration registers many routes and verifies matching.
func TestMassRegistration(t *testing.T) {
	r := router.NewRouter()
	numRoutes := 100
	for i := 0; i < numRoutes; i++ {
		route := "/mass/" + strconv.Itoa(i)
		expected := "mass:" + strconv.Itoa(i)
		err := r.Get(route, func(expected string) router.HandlerFunc {
			return func(w http.ResponseWriter, r *http.Request) error {
				_, err := w.Write([]byte(expected))
				return err
			}
		}(expected))
		if err != nil {
			t.Fatalf("failed to register route %q: %v", route, err)
		}
	}

	// Test a few random routes
	testIndices := []int{0, 10, 50, 99}
	for _, i := range testIndices {
		route := "/mass/" + strconv.Itoa(i)
		expected := "mass:" + strconv.Itoa(i)
		req := httptest.NewRequest("GET", route, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		resp := w.Result()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("failed to read response for route %q: %v", route, err)
		}
		if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
			t.Errorf("expected %q for route %q, got %q (status %d)", expected, route, body, resp.StatusCode)
		}
	}
}

// TestOverlappingRoutes tests overlapping static and dynamic routes.
func TestOverlappingRoutes(t *testing.T) {
	r := router.NewRouter()
	// 静的ルート
	err := r.Get("/overlap/test", func(w http.ResponseWriter, r *http.Request) error {
		_, err := w.Write([]byte("static overlap"))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register static overlap route: %v", err)
	}
	// 動的ルート
	err = r.Get("/overlap/{id}", func(w http.ResponseWriter, r *http.Request) error {
		ps := router.GetParams(r.Context())
		id, _ := ps.Get("id")
		_, err := w.Write([]byte("dynamic overlap:" + id))
		return err
	})
	if err != nil {
		t.Fatalf("failed to register dynamic overlap route: %v", err)
	}

	// リクエスト /overlap/test は静的ルートにマッチするはず
	req := httptest.NewRequest("GET", "/overlap/test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp := w.Result()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected := "static overlap"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q for /overlap/test, got %q (status %d)", expected, body, resp.StatusCode)
	}

	// リクエスト /overlap/123 は動的ルートにマッチするはず
	req = httptest.NewRequest("GET", "/overlap/123", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	resp = w.Result()
	body, err = io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response: %v", err)
	}
	expected = "dynamic overlap:123"
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != expected {
		t.Errorf("expected %q for /overlap/123, got %q (status %d)", expected, body, resp.StatusCode)
	}
}
