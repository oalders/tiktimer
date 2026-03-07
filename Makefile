BINARY := tiktimer
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

.PHONY: build clean release

build:
	CGO_ENABLED=1 go build -o $(BINARY) .

release: clean
	CGO_ENABLED=1 GOARCH=amd64 go build -o $(BINARY)-amd64 .
	CGO_ENABLED=1 GOARCH=arm64 go build -o $(BINARY)-arm64 .
	lipo -create -output $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64
	rm $(BINARY)-amd64 $(BINARY)-arm64

clean:
	rm -f $(BINARY) $(BINARY)-amd64 $(BINARY)-arm64
