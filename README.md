# CTFsh - CTF SSH Server

A secure SSH server for hosting Capture The Flag (CTF) challenges with a beautiful terminal user interface.

## Project Structure

This project follows standard Go project layout:

```
ctfsh/
├── cmd/ctfsh/           # Main application entry point
├── internal/            # Private application code
│   ├── config/          # Configuration constants
│   ├── database/        # Database operations and models
│   ├── download/        # SFTP and file handling
│   ├── instance/        # Container orchestration
│   ├── models/          # Data structures
│   ├── ui/             # Bubble Tea TUI interface
│   └── util/           # Utility functions
├── chals/              # CTF challenge files (ignored in refactor)
└── README.md
```

## Features

- **SSH Authentication**: Secure public key authentication
- **Terminal UI**: Beautiful Bubble Tea interface
- **Challenge Management**: View, download, and submit flags
- **Team System**: Create and join teams
- **Scoreboard**: Real-time team rankings
- **Container Support**: Docker-based challenge isolation
- **SFTP**: Secure file transfer for challenge downloads

## Architecture

The application is organized into discrete modules:

- **Database Layer**: Handles all data persistence and business logic
- **UI Layer**: Manages the terminal user interface
- **Instance Layer**: Orchestrates container management
- **Download Layer**: Handles file operations and SFTP
- **Config Layer**: Centralized configuration management
- **Models Layer**: Data structures and types

## Usage

1. Start the server: `go run cmd/ctfsh/main.go`
2. Connect via SSH: `ssh -p 2223 localhost`
3. Register with your SSH key and choose a username
4. Navigate the TUI to access challenges and team features

## Development

The refactored codebase is organized for maintainability and follows Go best practices:

- Clear separation of concerns
- Dependency injection
- Interface-based design
- Comprehensive error handling
- Modular architecture
