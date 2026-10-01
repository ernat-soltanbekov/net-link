.PHONY: build test audit stress fmt clean
build:
	go build -o TCPChat .
test:
	go vet ./...
	go test -race ./...
	python3 scripts/check_imports.py
audit: build test
	python3 scripts/audit.py --tui
stress: build test
	python3 scripts/audit.py --stress --tui
fmt:
	gofmt -w main.go main_test.go server client internal
clean:
	rm -f TCPChat coverage.out
