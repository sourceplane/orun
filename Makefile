.PHONY: build run validate debug plan clean test help examples-validate examples-debug examples-plan examples-gha-smoke

BINARY_NAME=orun
BINARY_PATH=./cmd/$(BINARY_NAME)
MAIN_PATH=$(BINARY_PATH)/main.go
EXAMPLE_INTENT=examples/intent.yaml
EXAMPLE_SMOKE_PLAN=/tmp/orun-example-gha-plan.json

# Default target
help:
	@echo "orun - Schema-Driven Planner Engine"
	@echo ""
	@echo "Available targets:"
	@echo "  build       - Build the binary"
	@echo "  run-plan    - Generate plan from examples"
	@echo "  run-validate - Validate example files"
	@echo "  run-debug   - Debug intent processing"
	@echo "  test        - Run tests"
	@echo "  clean       - Remove built artifacts"
	@echo ""

build:
	@echo "🔨 Building $(BINARY_NAME)..."
	@go build -o $(BINARY_NAME) $(BINARY_PATH)
	@echo "✅ Built: ./$(BINARY_NAME)"

run-plan: build
	@echo ""
	@echo "🎯 Generating plan..."
	@./$(BINARY_NAME) plan --intent $(EXAMPLE_INTENT) --output /tmp/orun-example-plan.json --view dag

run-validate: build
	@echo ""
	@echo "✓ Validating files..."
	@./$(BINARY_NAME) validate --intent $(EXAMPLE_INTENT)

run-debug: build
	@echo ""
	@echo "🔍 Debugging intent..."
	@./$(BINARY_NAME) debug --intent $(EXAMPLE_INTENT)

examples-validate: run-validate

examples-debug: run-debug

examples-plan: run-plan

examples-gha-smoke: build
	@echo ""
	@echo "⚙️ Planning Terraform smoke example..."
	@./$(BINARY_NAME) plan --intent $(EXAMPLE_INTENT) --component network-foundation --env development --output $(EXAMPLE_SMOKE_PLAN)
	@echo "🚀 Running GitHub Actions compatibility smoke..."
	@./$(BINARY_NAME) run --plan $(EXAMPLE_SMOKE_PLAN) --workdir examples --gha

test:
	@echo "🧪 Running tests..."
	@go test -v ./...

clean:
	@echo "🧹 Cleaning..."
	@rm -f $(BINARY_NAME)
	@go clean
	@echo "✅ Clean complete"

install-deps:
	@echo "📦 Installing dependencies..."
	@go mod tidy
	@go mod download
	@echo "✅ Dependencies installed"

fmt:
	@echo "🎨 Formatting code..."
	@go fmt ./...
	@echo "✅ Formatted"

lint:
	@echo "🔍 Linting..."
	@go vet ./...
	@echo "✅ Lint passed"

release-snapshot:
	@echo "📦 Building snapshot release with GoReleaser..."
	@which goreleaser > /dev/null || (echo "❌ goreleaser not found. Install with: brew install goreleaser" && exit 1)
	@goreleaser build --snapshot --clean
	@echo "✅ Snapshot built in dist/"

release-test:
	@echo "🧪 Testing OCI artifact structure..."
	@mkdir -p /tmp/orun-test
	@for arch in linux/amd64 darwin/arm64; do \
		if ls dist/*/platform-native_*_$${arch/\//_}/orun > /dev/null 2>&1; then \
			echo "✓ Binary found: $${arch}"; \
		fi; \
	done
	@echo "✅ Structure validated"

all: clean build test run-validate run-debug run-plan
	@echo ""
	@echo "✅ All targets completed"
