BINARY := tiktimer
APP := TikTimer.app
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

# menuet's Objective-C sources call the deprecated NSUserNotification API,
# producing clang deprecation warnings on every cgo build. Silence just that
# category so real warnings stay visible.
export CGO_CFLAGS := -g -O2 -Wno-deprecated-declarations

.PHONY: build app clean release

build:
	CGO_ENABLED=1 go build -o $(BINARY) .

app: build
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp $(BINARY) $(APP)/Contents/MacOS/
	cp Info.plist $(APP)/Contents/
	cp tiktimer.icns $(APP)/Contents/Resources/

release: clean
	CGO_ENABLED=1 GOARCH=amd64 go build -o $(BINARY)-amd64 .
	CGO_ENABLED=1 GOARCH=arm64 go build -o $(BINARY)-arm64 .
	lipo -create -output $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64
	rm $(BINARY)-amd64 $(BINARY)-arm64
	mkdir -p $(APP)/Contents/MacOS $(APP)/Contents/Resources
	cp $(BINARY) $(APP)/Contents/MacOS/
	cp Info.plist $(APP)/Contents/
	cp tiktimer.icns $(APP)/Contents/Resources/
	zip -r $(APP).zip $(APP)

clean:
	rm -rf $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64 $(APP) $(APP).zip
