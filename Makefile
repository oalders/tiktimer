BINARY := tiktimer
APP := TikTimer.app
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# menuet's Objective-C sources call the deprecated NSUserNotification API,
# producing clang deprecation warnings on every cgo build. Silence just that
# category so real warnings stay visible.
export CGO_CFLAGS := -g -O2 -Wno-deprecated-declarations

.PHONY: help init test build run app clean release

.DEFAULT_GOAL := help

# Print this help. Targets are documented with a trailing '## ' comment.
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-10s\033[0m %s\n", $$1, $$2}'

# Point git at the version-controlled hooks in .githooks (relative path, so it
# works regardless of where the repo is cloned). Run once after cloning.
init: ## Configure git to use the .githooks directory (run once after cloning)
	git config core.hooksPath .githooks

test: ## Run the Go test suite
	go test ./...

build: ## Build the tiktimer binary
	CGO_ENABLED=1 go build -o $(BINARY) .

run: app ## Build the app bundle and launch it for local development
	open $(APP)

app: build ## Build the binary and assemble TikTimer.app
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp $(BINARY) $(APP)/Contents/MacOS/
	cp Info.plist $(APP)/Contents/
	cp tiktimer.icns $(APP)/Contents/Resources/

release: clean ## Build a universal binary and package/zip TikTimer.app
	CGO_ENABLED=1 GOARCH=amd64 go build -o $(BINARY)-amd64 .
	CGO_ENABLED=1 GOARCH=arm64 go build -o $(BINARY)-arm64 .
	lipo -create -output $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64
	rm $(BINARY)-amd64 $(BINARY)-arm64
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp $(BINARY) $(APP)/Contents/MacOS/
	cp Info.plist $(APP)/Contents/
	cp tiktimer.icns $(APP)/Contents/Resources/
	zip -r $(APP).zip $(APP)

clean: ## Remove build artifacts
	rm -rf $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64 $(APP) $(APP).zip
