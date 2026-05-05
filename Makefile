BINARY   = morpheus-snapshot
VERSION  = 1.0.0
BUILD_DIR = dist
CMD      = ./cmd/server

.PHONY: all linux windows mac clean run

all: linux windows mac

linux:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-amd64 $(CMD)
	@echo "✓ Built: $(BUILD_DIR)/$(BINARY)-linux-amd64"

linux-arm:
	@mkdir -p $(BUILD_DIR)
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-linux-arm64 $(CMD)
	@echo "✓ Built: $(BUILD_DIR)/$(BINARY)-linux-arm64"

windows:
	@mkdir -p $(BUILD_DIR)
	GOOS=windows GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-windows-amd64.exe $(CMD)
	@echo "✓ Built: $(BUILD_DIR)/$(BINARY)-windows-amd64.exe"

mac:
	@mkdir -p $(BUILD_DIR)
	GOOS=darwin GOARCH=amd64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-amd64 $(CMD)
	GOOS=darwin GOARCH=arm64 go build -ldflags="-s -w -X main.version=$(VERSION)" -o $(BUILD_DIR)/$(BINARY)-darwin-arm64 $(CMD)
	@echo "✓ Built: macOS binaries"

run:
	go run $(CMD)/main.go

dev:
	PORT=8443 go run $(CMD)/main.go

clean:
	rm -rf $(BUILD_DIR) cert.pem key.pem

.PHONY: vet
vet:
	go vet ./...
