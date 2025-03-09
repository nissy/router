.PHONY: all

.PHONY: test
test:
	go clean -testcache && go test -race -v .
