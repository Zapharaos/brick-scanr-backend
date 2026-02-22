![GitHub Release](https://img.shields.io/github/v/release/Zapharaos/brick-scanr-backend)
![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/Zapharaos/brick-scanr-backend/golang.yml)
[![codecov](https://codecov.io/gh/Zapharaos/brick-scanr-backend/graph/badge.svg?token=BL7YP0GTK9)](https://codecov.io/gh/Zapharaos/brick-scanr-backend)

![GitHub License](https://img.shields.io/github/license/Zapharaos/brick-scanr-backend)
[![Go Report Card](https://goreportcard.com/badge/github.com/Zapharaos/brick-scanr-backend)](https://goreportcard.com/report/github.com/Zapharaos/brick-scanr-backend)

# BrickScanr Backend

**BrickScanr Backend** is a high-performance RESTful API service that powers the BrickScanr application. Built with Go, it provides comprehensive data aggregation and real-time search capabilities for LEGO® sets and bricks, integrating multiple data sources including BrickLink, LEGO Pick-a-Brick, and official LEGO APIs.

## 🎯 What is BrickScanr Backend?

The backend service provides:
- **Search** for LEGO sets and individual bricks
- **Browse** detailed information about sets including part inventories and instructions
- **Analyze** pricing data
- **Track** availability
- **Export** data for personal use or analysis
- **Caching Layer** - Redis-based caching for optimal performance
- **Rate Limiting** - Adaptive throttling to respect external API limits
- **Multi-language Support** - Internationalized responses

## 🛠️ Technologies

Built with modern Go technologies and best practices:

- **Go 1.24** - High-performance compiled language
- **Gin** - Fast HTTP web framework
- **Redis** - In-memory caching and distributed locking
- **Viper** - Configuration management
- **Zap** - Structured, high-performance logging
- **Swagger/OpenAPI** - API documentation and client generation
- **Docker** - Containerized deployment

### Key Features

- **Adaptive Rate Limiting** - Smart throttling based on API response times
- **Concurrent Processing** - Efficient goroutine-based request handling
- **Distributed Locking** - Redsync for coordinated cache updates
- **Health Monitoring** - Database connection health checks
- **Swagger Documentation** - Auto-generated API documentation

## 🚀 Getting Started

### Prerequisites

- **Go**: 1.24 or higher
- **Docker**: 20.x or higher (for containerized deployment)
- **Docker Compose**: 2.x or higher
- **Make**: For build automation (optional but recommended)
- **Redis**: 7.x or higher (included in Docker setup)

### Prerequisites

- **Go**: 1.24 or higher
- **Docker**: 20.x or higher (for containerized deployment)
- **Docker Compose**: 2.x or higher
- **Make**: For build automation (optional but recommended)
- **Redis**: 7.x or higher (included in Docker setup)

### Installation

1. Clone the repository:
```bash
git clone https://github.com/Zapharaos/brick-scanr-backend.git
cd brick-scanr-backend
```

2. Install Go dependencies:
```bash
go mod download
```

3. Install build tools:

```bash
# Make (if not already installed)
sudo apt-get install make # Linux/WSL
choco install make # Windows (not recommended, some commands may not work)

# Swagger CLI for API documentation
go install github.com/swaggo/swag/cmd/swag@latest
```

4. Configure environment:
```bash
# Copy example environment file
cp .env.example .env

# Edit .env with your configuration
# Set COMPOSE_FILE, APP_ENV, ports, and Redis password
```

## 💻 Development

### Production (with Docker)

This project uses Docker. Get started [here](https://www.docker.com/get-started).

#### Build

To build the project:
```bash
make build
```

#### Start

To start the whole project:
```bash
docker compose up
# add -d to run in detached mode
# add --build to rebuild the images
```

**Configuration:** Set your environment in `.env` file (see `.env.example`).

---

## 💻 Development

### Development (without Docker)

#### IDE - GoLand (recommended)

We recommend using GoLand for debugging. See [Run/debug configuration](https://www.jetbrains.com/help/go/run-debug-configuration.html).

- Create a new `Go build` configuration
- Set `Package path` to `github.com/Zapharaos/brick-scanr-backend`
- Enable `Run after build`
- Set `Working directory` to the root of the project `brick-scanr-backend`
- Add `Environment variables` to override any config variables you need
  - If using local Redis: `BRICK_SCANR_REDIS_HOST=localhost`
  - For production mode: `APP_ENV=prod`
  - Any other overrides: `BRICK_SCANR_*`

#### Command line (not recommended)

You can run with the following command:
```bash
go run .
```

---

### Manual Build

```bash
# Build executable
make build

# Run in development (default)
./bin/brick-scanr-backend

# Run in production
APP_ENV=prod ./bin/brick-scanr-backend
```

---

### Configuration

**Configuration Files:**
- **`config/config.yaml`** - Default configuration (development)
- **`config/config.prod.yaml`** - Production overrides (loaded when `APP_ENV=prod`)

**Docker Compose Files:**
- **`docker-compose.dev.yml`** - Development setup
- **`docker-compose.prod.yml`** - Production setup with VPS network

**Environment Variables (in .env):**
- `COMPOSE_FILE` - Which docker-compose file to use (`docker-compose.dev.yml` or `docker-compose.prod.yml`)
- `APP_ENV` - Set to `prod` for production, leave empty for development
- `BACKEND_PORT` - Backend port (3000 for dev, 3002 for prod)
- `REDIS_PORT` - Redis port (6379 for dev, 6380 for prod)
- `BRICK_SCANR_*` - Any config overrides

**Port Reference:**

| Environment | COMPOSE_FILE | APP_ENV | BACKEND_PORT | REDIS_PORT |
|-------------|--------------|---------|--------------|------------|
| **Development** | `docker-compose.dev.yml` | _(empty)_ | 3000 | 6379 |
| **Production** | `docker-compose.prod.yml` | `prod` | 3002 | 6380 |

---

### Swagger Generation

Generate the swagger file (reused by frontend):

```bash
make swagger
```

## 🔧 Configuration Files

| File | Purpose |
|------|---------|
| `config/config.yaml` | Base configuration (development defaults) |
| `config/config.prod.yaml` | Production-specific overrides |
| `.env` | Environment variables for Docker Compose |
| `.env.example` | Example environment configuration |
| `docker-compose.dev.yml` | Development Docker setup |
| `docker-compose.prod.yml` | Production Docker setup with VPS network |
| `Dockerfile` | Container image definition |
| `Makefile` | Build automation scripts |
| `go.mod` | Go module dependencies |

## 📜 Scripts

```bash
make build          # Build Go executable
make run            # Run compiled executable
make swagger        # Generate Swagger documentation
make swagger-init   # Initialize Swagger YAML
make swagger-ui     # Serve Swagger UI
make swagger-gen    # Generate TypeScript client
make help           # Show all available commands
```

## 🤝 Contributing

Contributions are welcome! Please follow these guidelines:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

### Code Style

- Follow Go best practices and [Effective Go](https://go.dev/doc/effective_go)
- Use `gofmt` to format code
- Run `go vet` to check for common issues
- Write unit tests for new features
- Ensure all tests pass before submitting PR (`go test ./...`)
- Update Swagger documentation for API changes

### Testing

Run tests with:
```bash
go test ./...
```

Run tests with coverage:
```bash
go test -cover ./...
```

## 📄 License

This project is licensed under the **MIT License** - see the [LICENSE](LICENSE) file for details.

Copyright (c) 2026 Matthieu FREITAG

## 📚 Additional Resources

- [Go Documentation](https://go.dev/doc/)
- [Gin Web Framework](https://gin-gonic.com/docs/)
- [Redis Documentation](https://redis.io/docs/)
- [Viper Configuration](https://github.com/spf13/viper)
- [Zap Logging](https://github.com/uber-go/zap)
- [Swagger/OpenAPI](https://swagger.io/docs/)

## ⚠️ Disclaimer

LEGO® is a trademark of the LEGO Group, which does not sponsor, authorize, or endorse this application.

BrickLink® is a trademark of the LEGO Group. This application may use data from BrickLink but is not affiliated with, endorsed by, or sponsored by BrickLink or the LEGO Group.
