.PHONY: build build-wpe build-obscura test test-static test-browser bench clean

NPM ?= npm

build:
	$(NPM) run build
	go generate ./internal/rush
	mkdir -p bin
	go build -o bin/rush ./cmd/rush

build-wpe:
	@pkg-config --atleast-version=2.52 wpe-webkit-2.0 wpe-platform-headless-2.0 || (echo "WPE WebKit 2.52+ development files with the headless platform are required" >&2; exit 1)
	mkdir -p bin/wpe
	$(CC) -shared -fPIC -O2 -o bin/wpe/libwebview.so native/wpe/webview.c $$(pkg-config --cflags --libs wpe-webkit-2.0 wpe-platform-headless-2.0)
	go build -tags rush_wpe -o bin/wpe/rush ./cmd/rush

build-obscura:
	$(NPM) run build
	go generate ./internal/rush
	mkdir -p bin/obscura
	go build -tags rush_obscura -o bin/obscura/rush ./cmd/rush

test: test-static test-browser

test-static: build
	$(NPM) run check
	go test -race ./...

test-browser: build
	./bin/rush test test/*.test.ts

bench: build
	./bin/rush bench

clean:
	rm -rf bin
