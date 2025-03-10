.PHONY: build test test-race clean

# デフォルトのターゲット
all: build

# ビルド
build:
	go build ./...

# テスト
test:
	go test ./...

# race検出器を有効にしたテスト
test-race:
	go test -race ./...

# キャッシュをクリアしてテスト
test-clean:
	go clean -testcache && go test ./...

# キャッシュをクリアしてrace検出器を有効にしたテスト
test-race-clean:
	go clean -testcache && go test -race ./...

# クリーン
clean:
	go clean
