#!/bin/bash

echo "Testing CTF Container Manager..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed"
    exit 1
fi

# Check if required files exist
echo "Checking required files..."
files=("main.go" "go.mod" "chal/app.py" "chal/docker-compose.yml" "chal/Dockerfile")
for file in "${files[@]}"; do
    if [ ! -f "$file" ]; then
        echo "Error: Required file $file not found"
        exit 1
    fi
    echo "✓ $file"
done

# Build the application
echo "Building application..."
if go build -o ctf-manager; then
    echo "✓ Application built successfully"
else
    echo "Error: Failed to build application"
    exit 1
fi

# Check if binary was created
if [ -f "ctf-manager" ]; then
    echo "✓ Binary created: ctf-manager"
else
    echo "Error: Binary not created"
    exit 1
fi

# Test basic functionality (without running)
echo "Testing basic functionality..."

# Check if the binary has the expected structure
if file ctf-manager | grep -q "executable"; then
    echo "✓ Binary is executable"
else
    echo "Error: Binary is not executable"
    exit 1
fi

echo ""
echo "✓ All tests passed!"
echo ""
echo "To run the application:"
echo "  sudo ./ctf-manager"
echo ""
echo "To connect via SSH:"
echo "  ssh -p 2222 user@localhost"
echo ""
echo "To use port forwarding:"
echo "  ssh -p 2222 -L 8000:localhost:8000 user@localhost" 