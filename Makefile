PLUGIN_ID := quota-auto-balance
BUILD_DIR := dist

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

ifeq ($(GOOS),darwin)
	EXT := dylib
else ifeq ($(GOOS),windows)
	EXT := dll
else
	EXT := so
endif

OUTPUT := $(BUILD_DIR)/$(PLUGIN_ID)-$(GOOS)-$(GOARCH).$(EXT)
LOCAL_LIB := $(BUILD_DIR)/$(PLUGIN_ID).$(EXT)

.PHONY: all build test test-go test-ui clean

all: build

build:
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=1 go build -buildmode=c-shared -trimpath -ldflags="-s -w" -o $(OUTPUT) .
	@cp $(OUTPUT) $(LOCAL_LIB)
	@echo "Built $(OUTPUT) and $(LOCAL_LIB)"

test-go:
	go test -v ./...

test-ui:
	node --test ui_template_test.mjs

test: test-go test-ui

clean:
	rm -rf $(BUILD_DIR)
