# CTF Container Manager

A Golang application that manages LXC containers for CTF (Capture The Flag) challenges. This application provides an SSH server that dynamically creates isolated containers for each connection, runs Docker Compose services, and handles port forwarding.

## Features

### Initial Setup
- Creates a new Alpine LXC container connected to an isolated network
- Installs Docker and Docker Compose in the container
- Copies challenge files (`./chal` directory) into the container
- Runs `docker-compose up -d` to start services
- Stops the container to use as a template for future containers

### SSH Server
- Custom SSH server listening on port 2222
- Accepts any authentication (no password/key required)
- Creates a new container clone for each SSH connection
- Automatically starts Docker Compose services in each container

### Port Forwarding
- Supports SSH port forwarding with `-L` option
- Forwards TCP connections to requested destination ports in LXC containers
- Handles multiple concurrent port forwards per connection

### Container Management
- Automatic container cleanup when SSH connections close
- Isolated network access (internet but no access to other containers)
- Unique container names for each connection

## Architecture

The application is organized into several components:

### Main Components
- **ContainerManager**: Main orchestrator that manages SSH connections and containers
- **SSHForwardManager**: Manages SSH port forwarding functionality

### File Structure
```
deploy5/
├── main.go              # Main application entry point
├── ssh_forward.go       # SSH port forwarding
├── go.mod               # Go module dependencies
├── README.md            # This file
└── chal/                # Challenge files
    ├── app.py           # Python web application
    ├── docker-compose.yml # Docker Compose configuration
    └── Dockerfile       # Docker image definition
```

## Prerequisites

### System Requirements
- Linux system with LXC support
- LXC tools installed (`lxc-create`, `lxc-start`, `lxc-stop`, etc.)
- Docker installed on the host system
- Go 1.21 or later

### LXC Setup
```bash
# Install LXC tools (Ubuntu/Debian)
sudo apt-get install lxc lxc-templates

# Install LXC tools (CentOS/RHEL)
sudo yum install lxc lxc-templates

# Verify LXC installation
lxc-checkconfig
```

## Installation

1. Clone the repository:
```bash
git clone <repository-url>
cd deploy5
```

2. Install Go dependencies:
```bash
go mod tidy
```

3. Build the application:
```bash
go build -o ctf-manager
```

## Usage

### Running the Application

1. Start the CTF Container Manager:
```bash
sudo ./ctf-manager
```

The application will:
- Perform initial setup (create template container)
- Start SSH server on port 2222
- Log all activities

### Connecting to Containers

1. Connect via SSH (no authentication required):
```bash
ssh -p 2222 user@localhost
```

2. Use port forwarding to access services:
```bash
# Forward local port 8000 to container's port 8000
ssh -p 2222 -L 8000:localhost:8000 user@localhost

# Access the web application
curl http://localhost:8000/flag
```

### Container Lifecycle

1. **Connection**: When a new SSH connection is established:
   - A unique container name is generated (e.g., `ctf-abc12345`)
   - Container is cloned from the template
   - Container is started and Docker Compose services are launched
   - Container is ready for use

2. **Usage**: During the SSH session:
   - Port forwarding requests are handled automatically
   - Services run in isolation within the container
   - Network access is limited to internet only

3. **Cleanup**: When SSH connection closes:
   - All port forwards are closed
   - Container is stopped and destroyed
   - Resources are freed

## Configuration

### Constants (main.go)
```go
const (
    SSH_PORT = 2222                    // SSH server port
    LXC_BASE_NAME = "ctf-template"     // Template container name
    LXC_NETWORK_NAME = "ctf-network"   // LXC network name
    CHAL_DIR = "./chal"                // Challenge files directory
)
```

### LXC Network Configuration
The application creates an isolated LXC network with:
- Network type: `veth`
- Bridge: `lxcbr0`
- IP range: `10.0.3.0/24`
- Gateway: `10.0.3.1`

### Container Configuration
Each container includes:
- Alpine Linux base image
- Docker and Docker Compose installed
- Challenge files copied to `/chal` directory
- Network isolation (internet access only)
- Security profiles (seccomp, AppArmor)

## Security Considerations

### Container Isolation
- Each container runs in complete isolation
- No access to other containers or host system
- Network access limited to internet only
- Security profiles prevent privilege escalation

### SSH Security
- No authentication required (for CTF use case)
- Custom SSH server implementation
- Port forwarding limited to container services

### Resource Management
- Automatic cleanup prevents resource exhaustion
- Unique container names prevent conflicts
- Timeout handling for container startup

## Troubleshooting

### Common Issues

1. **LXC commands not found**:
   ```bash
   sudo apt-get install lxc lxc-templates
   ```

2. **Permission denied**:
   ```bash
   # Run with sudo for LXC operations
   sudo ./ctf-manager
   ```

3. **Container creation fails**:
   ```bash
   # Check LXC configuration
   lxc-checkconfig
   
   # Verify template availability
   lxc-create --list-templates
   ```

4. **Docker not starting in container**:
   ```bash
   # Check container logs
   lxc-attach -n <container-name> -- journalctl -u docker
   ```

### Logging
The application provides detailed logging for:
- Container lifecycle events
- SSH connection handling
- Port forwarding operations
- Error conditions

## Development

### Adding New Features
1. **New Container Types**: Modify `LXCManager` to support different base images
2. **Additional Services**: Update challenge files in `./chal` directory
3. **Custom Authentication**: Modify SSH server configuration in `setupSSH()`
4. **Monitoring**: Add metrics collection and health checks

### Testing
```bash
# Build and test
go build
go test ./...

# Run with debug logging
DEBUG=1 ./ctf-manager
```

## License

This project is designed for educational and CTF purposes. Use responsibly and in accordance with applicable laws and regulations. 