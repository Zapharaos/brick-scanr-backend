# App
APP ?= brick-scanr-backend
GOOS ?=
TAG ?= v0.0.0
BUILD ?= 0
BUILD_DATE = $(shell date +%FT%T)

# Go variables
GO111MODULE ?= on
GOPROXY ?= "https://proxy.golang.org,direct"
GOSUMDB ?= "sum.golang.org"
CGO_ENABLED ?= 0
GOINSECURE ?=
GONOSUMDB ?=
GO_OPT=GOPROXY=$(GOPROXY) GOINSECURE=$(GOINSECURE) GONOSUMDB=$(GONOSUMDB) GOSUMDB=$(GOSUMDB) GO111MODULE=$(GO111MODULE) CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS)

# Docker
DOCKER_COMPOSE = docker compose
DOCKER_FILE = docker-compose.yml
DOCKER_IMAGE ?= $(APP):$(TAG)

# Swagger
SWAGGER_FILE = docs/swagger.yaml
SWAGGER_UI_PORT = 80

# Build the executable
build:
	$(GO_OPT) go build -a -trimpath -ldflags "-X main.Version=$(TAG)-$(BUILD) -X main.BuildDate=$(BUILD_DATE)" -o bin/$(APP)

run: # Run the executable
	bin/$(APP)

# Build services
docker-build:
ifeq ($(TAG),v0.0.0)
	$(DOCKER_COMPOSE) --progress plain build
else
	$(DOCKER_COMPOSE) --progress plain build --build-arg TAG=$(TAG)
endif

# Swagger commands
swagger: swagger-init swagger-ui swagger-gen

swagger-init:
	swag init -d cmd/api,internal -ot yaml

swagger-ui:
	docker run --rm -d -p $(SWAGGER_UI_PORT):8080 -e SWAGGER_JSON=/tmp/swagger.yaml -v `pwd`/docs:/tmp swaggerapi/swagger-ui
	@echo "Swagger UI is available at http://localhost:$(SWAGGER_UI_PORT)"

swagger-gen:
	docker run --rm -v `pwd`:/local openapitools/openapi-generator-cli:v7.11.0 generate -i /local/docs/swagger.yaml -g typescript-angular -o /local/docs/angular

# Help command to display usage
help:
	@echo "Usage:"
	@echo "  make build        		  \- Build Go executable"
	@echo "  make run        		  \- Run Go executable"
	@echo "  make docker-build        \- Build all Docker services"
	@echo "  make swagger             \- Generate and serve Swagger documentation"
	@echo "  make swagger-init        \- Initialize Swagger documentation"
	@echo "  make swagger-ui          \- Serve Swagger UI"
	@echo "  make swagger-gen         \- Generate TypeScript Angular client from Swagger"
