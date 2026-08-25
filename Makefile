.PHONY: build test bench clean

build:
	mkdir -p bin
	go build -o bin/rush ./cmd/rush

test:
	go test ./...

bench: build
	./bin/rush bench

clean:
	-go run ./cmd/rush stop
	rm -rf bin
