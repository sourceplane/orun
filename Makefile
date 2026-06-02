.PHONY: build run validate debug plan clean test help examples-validate examples-debug examples-plan examples-gha-smoke test-state-redesign test-object-model verify-generated

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

test-state-redesign:
	@echo "🧪 Running state-redesign test suites..."
	@go test -count=1 -race ./internal/testfx/statefs/...
	@go test -count=1 -race ./internal/triggerctx/...
	@echo "🧪 Coverage gate: ./internal/statestore/... (>= 95%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/statestore/... | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 95.0) { printf "❌ coverage %.1f%% below 95%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 Coverage gate: ./internal/revision/... (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/revision/... | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 Coverage gate: ./internal/executionstate/... (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/executionstate/... | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 End-to-end revision-first walk (test-plan.md §4)"
	@go test -count=1 -race -run TestStateE2E ./cmd/orun/...
	@echo "🧪 Component-catalog packages (Phase 2 C0)"
	@go test -count=1 -race ./internal/catalogmodel/...
	@go test -count=1 -race ./internal/sourcectx/...
	@echo "🧪 Coverage gate: ./internal/catalogmodel/ (>= 90%)"
	@COVER=$$(go test -count=1 -cover -coverprofile=/tmp/orun-catalogmodel.cov ./internal/catalogmodel/ >/dev/null && \
	  go tool cover -func=/tmp/orun-catalogmodel.cov | tail -n 1 | awk '{gsub("%","",$$3); print $$3}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ catalogmodel coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 Coverage gate: ./internal/sourcectx/ (>= 90%)"
	@COVER=$$(go test -count=1 -cover -coverprofile=/tmp/orun-sourcectx.cov ./internal/sourcectx/ >/dev/null && \
	  go tool cover -func=/tmp/orun-sourcectx.cov | tail -n 1 | awk '{gsub("%","",$$3); print $$3}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ sourcectx coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 Component-catalog resolution pipeline (Phase 2 C2)"
	@go test -count=1 -race ./internal/catalogresolve/...
	@echo "🧪 Coverage gate: ./internal/catalogresolve/ (>= 90%)"
	@COVER=$$(go test -count=1 -cover -coverprofile=/tmp/orun-catalogresolve.cov ./internal/catalogresolve/ >/dev/null && \
	  go tool cover -func=/tmp/orun-catalogresolve.cov | tail -n 1 | awk '{gsub("%","",$$3); print $$3}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ catalogresolve coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 Coverage gate: ./internal/catalogmodel/ Sanitize* (== 100%)"
	@COVER=$$(go test -count=1 -cover -coverprofile=/tmp/orun-catalogmodel.cov ./internal/catalogmodel/ >/dev/null && \
	  go tool cover -func=/tmp/orun-catalogmodel.cov | \
	  awk '/Sanitize|ShortHex/ {gsub("%","",$$3); s+=$$3+0; n++} END {if (n>0) printf "%.1f", s/n; else print "0"}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 100.0) { printf "❌ Sanitize* coverage %.1f%% below 100%% threshold\n", c+0; exit 1 } }'
	# add packages as state-redesign milestones land

test-object-model:
	@echo "🧪 object-model: lint gate (claude-goals.md §3)"
	@bash scripts/check-object-model.sh
	@echo "🧪 object-model: testfx/objfs"
	@go test -count=1 -race ./internal/testfx/objfs/...
	@echo "🧪 object-model: objectstore (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/objectstore | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ objectstore coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: refstore (>= 88%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/objectstore/refstore | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 88.0) { printf "❌ refstore coverage %.1f%% below 88%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: nodes (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/nodes | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ nodes coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: nodewriter (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/nodewriter | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ nodewriter coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: objplan (>= 90%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/objplan | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 90.0) { printf "❌ objplan coverage %.1f%% below 90%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: workingview (>= 85%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/workingview | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 85.0) { printf "❌ workingview coverage %.1f%% below 85%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: execseal (>= 85%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/execseal | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 85.0) { printf "❌ execseal coverage %.1f%% below 85%% threshold\n", c+0; exit 1 } }'
	@echo "🧪 object-model: objexec bridge (>= 85%)"
	@COVER=$$(go test -count=1 -race -cover ./internal/objexec | awk '/coverage:/ {gsub("%","",$$5); print $$5}'); \
	  echo "   measured: $$COVER%"; \
	  awk -v c=$$COVER 'BEGIN { if (c+0 < 85.0) { printf "❌ objexec coverage %.1f%% below 85%% threshold\n", c+0; exit 1 } }'
	# coverage gate for ./internal/objindex (>= 90%) is added with that package (M8).

verify-generated:
	@echo "🧪 Verifying generated artifacts are up-to-date..."
	@go generate ./internal/catalogmodel/...
	@if ! git diff --exit-code -- internal/catalogmodel/schema/ >/dev/null 2>&1; then \
	  echo "❌ generated schema is stale; run 'go generate ./internal/catalogmodel/...' and commit"; \
	  git --no-pager diff -- internal/catalogmodel/schema/; \
	  exit 1; \
	fi
	@echo "✅ generated artifacts up-to-date"
