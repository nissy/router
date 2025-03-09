package router

import (
	"testing"
)

// TestRouterError はRouterErrorの作成と文字列変換をテストします
func TestRouterError(t *testing.T) {
	// 新しいRouterErrorを作成
	err := &RouterError{
		Code:    ErrInvalidPattern,
		Message: "テストエラーメッセージ",
	}

	// エラーコードをチェック
	if err.Code != ErrInvalidPattern {
		t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidPattern, err.Code)
	}

	// エラーメッセージをチェック
	if err.Message != "テストエラーメッセージ" {
		t.Errorf("エラーメッセージが異なります。期待値: %s, 実際: %s", "テストエラーメッセージ", err.Message)
	}

	// エラーの文字列表現をチェック
	expected := "InvalidPattern: テストエラーメッセージ"
	if err.Error() != expected {
		t.Errorf("エラーの文字列表現が異なります。期待値: %s, 実際: %s", expected, err.Error())
	}
}

// TestErrorCodes はエラーコードの定義をテストします
func TestErrorCodes(t *testing.T) {
	// エラーコードの定義をチェック
	if ErrInvalidPattern != 1 {
		t.Errorf("ErrInvalidPatternの値が異なります。期待値: %d, 実際: %d", 1, ErrInvalidPattern)
	}

	if ErrInvalidMethod != 2 {
		t.Errorf("ErrInvalidMethodの値が異なります。期待値: %d, 実際: %d", 2, ErrInvalidMethod)
	}

	if ErrNilHandler != 3 {
		t.Errorf("ErrNilHandlerの値が異なります。期待値: %d, 実際: %d", 3, ErrNilHandler)
	}

	if ErrInternalError != 4 {
		t.Errorf("ErrInternalErrorの値が異なります。期待値: %d, 実際: %d", 4, ErrInternalError)
	}
}

// TestValidateMethod はHTTPメソッドの検証をテストします
func TestValidateMethod(t *testing.T) {
	// 有効なHTTPメソッド
	validMethods := []string{
		"GET",
		"POST",
		"PUT",
		"DELETE",
		"PATCH",
		"HEAD",
		"OPTIONS",
	}

	// 無効なHTTPメソッド
	invalidMethods := []string{
		"",
		"INVALID",
		"get", // 小文字は無効
		"CONNECT",
		"TRACE",
	}

	// 有効なメソッドをテスト
	for _, method := range validMethods {
		err := validateMethod(method)
		if err != nil {
			t.Errorf("有効なメソッド %s が無効と判定されました: %v", method, err)
		}
	}

	// 無効なメソッドをテスト
	for _, method := range invalidMethods {
		err := validateMethod(method)
		if err == nil {
			t.Errorf("無効なメソッド %s が有効と判定されました", method)
		}

		// エラーの種類をチェック
		routerErr, ok := err.(*RouterError)
		if !ok {
			t.Errorf("期待されるエラータイプではありません: %T", err)
			continue
		}

		if routerErr.Code != ErrInvalidMethod {
			t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidMethod, routerErr.Code)
		}
	}
}

// TestValidatePattern はルートパターンの検証をテストします
func TestValidatePattern(t *testing.T) {
	// 有効なパターン
	validPatterns := []string{
		"/",
		"/users",
		"/users/{id}",
		"/users/{id}/profile",
		"/users/{id:[0-9]+}",
		"/api/v1/users",
	}

	// 無効なパターン
	invalidPatterns := []string{
		"", // 空文字列
	}

	// 有効なパターンをテスト
	for _, pattern := range validPatterns {
		err := validatePattern(pattern)
		if err != nil {
			t.Errorf("有効なパターン %s が無効と判定されました: %v", pattern, err)
		}
	}

	// 無効なパターンをテスト
	for _, pattern := range invalidPatterns {
		err := validatePattern(pattern)
		if err == nil {
			t.Errorf("無効なパターン %s が有効と判定されました", pattern)
		}

		// エラーの種類をチェック
		routerErr, ok := err.(*RouterError)
		if !ok {
			t.Errorf("期待されるエラータイプではありません: %T", err)
			continue
		}

		if routerErr.Code != ErrInvalidPattern {
			t.Errorf("エラーコードが異なります。期待値: %d, 実際: %d", ErrInvalidPattern, routerErr.Code)
		}
	}
}

// TestParseSegments はパスセグメントの解析をテストします
func TestParseSegments(t *testing.T) {
	// テストケース
	tests := []struct {
		path           string
		expectedResult []string
	}{
		{"/", []string{""}},
		{"/users", []string{"users"}},
		{"/users/profile", []string{"users", "profile"}},
		{"/users/{id}", []string{"users", "{id}"}},
		{"/users/{id}/profile", []string{"users", "{id}", "profile"}},
		{"/api/v1/users", []string{"api", "v1", "users"}},
	}

	// 各テストケースを実行
	for _, tt := range tests {
		result := parseSegments(tt.path)

		// 結果の長さをチェック
		if len(result) != len(tt.expectedResult) {
			t.Errorf("パス %s のセグメント数が異なります。期待値: %d, 実際: %d", tt.path, len(tt.expectedResult), len(result))
			continue
		}

		// 各セグメントをチェック
		for i, expected := range tt.expectedResult {
			if result[i] != expected {
				t.Errorf("パス %s のセグメント %d が異なります。期待値: %s, 実際: %s", tt.path, i, expected, result[i])
			}
		}
	}
}

// TestIsAllStatic はすべてのセグメントが静的かどうかをテストします
func TestIsAllStatic(t *testing.T) {
	// テストケース
	tests := []struct {
		segments       []string
		expectedResult bool
	}{
		{[]string{""}, true},
		{[]string{"users"}, true},
		{[]string{"users", "profile"}, true},
		{[]string{"users", "{id}"}, false},
		{[]string{"users", "{id}", "profile"}, false},
		{[]string{"users", "{id:[0-9]+}"}, false},
		{[]string{"api", "v1", "users"}, true},
	}

	// 各テストケースを実行
	for _, tt := range tests {
		result := isAllStatic(tt.segments)

		// 結果をチェック
		if result != tt.expectedResult {
			t.Errorf("セグメント %v の静的判定が異なります。期待値: %t, 実際: %t", tt.segments, tt.expectedResult, result)
		}
	}
}
