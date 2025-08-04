# CTF Container Manager Makefile

.PHONY: build clean test run help

# Default target
all: build

# Build the application
build:
	@echo "Building CTF Container Manager..."
	go build -o ctf-manager
	@echo "✓ Build complete"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f ctf-manager
	@echo "✓ Clean complete"

# Run tests
test:
	@echo "Running tests..."
	./test.sh

# Run the application (requires sudo for LXC operations)
run: build
	@echo "Starting CTF Container Manager..."
	@echo "Note: This requires sudo privileges for LXC operations"
	sudo ./ctf-manager

# Install dependencies
deps:
	@echo "Installing Go dependencies..."
	go mod tidy
	@echo "✓ Dependencies installed"

# Show help
help:
	@echo "CTF Container Manager - Available commands:"
	@echo ""
	@echo "  build   - Build the application"
	@echo "  clean   - Remove build artifacts"
	@echo "  test    - Run tests"
	@echo "  run     - Build and run the application (requires sudo)"
	@echo "  deps    - Install Go dependencies"
	@echo "  help    - Show this help message"
	@echo ""
	@echo "Usage examples:"
	@echo "  make build    # Build the application"
	@echo "  make run      # Run with sudo privileges"
	@echo "  make test     # Run tests"
	@echo ""
	@echo "SSH Connection:"
	@echo "  ssh -p 2222 user@localhost"
	@echo ""
	@echo "Port Forwarding:"
	@echo "  ssh -p 2222 -L 8000:localhost:8000 user@localhost" 