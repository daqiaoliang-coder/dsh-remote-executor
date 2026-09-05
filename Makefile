.PHONY: test run fmt
fmt:
	gofmt -w $$(find . -name '*.go' -not -path './vendor/*')
test:
	go test ./...
run:
	go run ./cmd/gateway
