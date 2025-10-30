![GitHub Release](https://img.shields.io/github/v/release/Zapharaos/brick-scanr-backend)
![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/Zapharaos/brick-scanr-backend/golang.yml)
[![codecov](https://codecov.io/gh/Zapharaos/brick-scanr-backend/graph/badge.svg?token=BL7YP0GTK9)](https://codecov.io/gh/Zapharaos/brick-scanr-backend)

![GitHub License](https://img.shields.io/github/license/Zapharaos/brick-scanr-backend)
[![Go Report Card](https://goreportcard.com/badge/github.com/Zapharaos/brick-scanr-backend)](https://goreportcard.com/report/github.com/Zapharaos/brick-scanr-backend)

# brick-scanr-backend

## Dependencies

- Makefile is used to run commands
```bash
sudo apt-get install make # If using WSL or any Linux distribution
choco install make # If using Powershell (not recommended, some commands may not work)
```

- Swagg is used to generate the swagger file. Install it with the following command:
```bash
go install github.com/swaggo/swag/cmd/swag@latest
```

## Running the project

### Production (with Docker)

This project is using Docker. Get started [here](https://www.docker.com/get-started).

#### Build

To build the project, you can use either of the following commands:
```bash
make build
```

#### Start

To start the whole project, you can use either of the following commands:
```bash
docker compose up
# add -d to run in detached mode
# add --build to rebuild the images
```

### Development (without Docker)

#### IDE - GoLand (recommended)

We recommend you to use GoLand as it is more convenient, especially for debugging. See [Run/debug configuration](https://www.jetbrains.com/help/go/run-debug-configuration.html).

- Start by creating a new `Go build` configuration.
- Set the `Package path` to `github.com/Zapharaos/brick-scanr-backend`.
- Enable `Run after build`.
- Set the `Working directory` to the root of the project `brick-scanr-backend`.
- Add `Environment variables` to override any config variables that you need.

#### Command line (not recommended)

You can run with the following command:
```bash
go run .
````

### Generation - Swagger

Generate the swagger file (reused by frontend).

```bash
make swagger
```