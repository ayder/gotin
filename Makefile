# Makefile for Gotin

# Variables
BINARY_NAME=gotin
CMD_DIR=./cmd/gotin
BUILD_DIR=./build

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build flags
LDFLAGS=-ldflags="-s -w"

.PHONY: all build clean test run tidy help

all: tidy build

build:
	@echo "Building $(BINARY_NAME)..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)
	@echo "Binary generated at $(BUILD_DIR)/$(BINARY_NAME)"

run: build
	$(BUILD_DIR)/$(BINARY_NAME)

test:
	@echo "Running tests..."
	$(GOTEST) -v ./...

clean:
	@echo "Cleaning up..."
	$(GOCLEAN)
	rm -rf $(BUILD_DIR)
	rm -f $(BINARY_NAME)
	@echo "Clean complete."

tidy:
	@echo "Tidying go modules..."
	$(GOMOD) tidy

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  all     - Tidy modules and build the binary (default)"
	@echo "  build   - Build the project binary"
	@echo "  run     - Build and run the project"
	@echo "  test    - Run all project tests"
	@echo "  clean   - Remove build artifacts and binary"
	@echo "  tidy    - Run go mod tidy"
	@echo "  help    - Show this help message"
