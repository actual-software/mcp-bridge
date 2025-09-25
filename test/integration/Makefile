.PHONY: test test-up test-down test-logs clean build-services

# Default target - run complete integration test suite
test:
	@echo "🚀 Running complete integration test suite..."
	docker-compose -f docker-compose.test.yml up --build --abort-on-container-exit
	@echo "🧹 Cleaning up test environment..."
	docker-compose -f docker-compose.test.yml down -v --remove-orphans

# Start services for testing (without running tests)
test-up:
	@echo "🚀 Starting integration test environment..."
	docker-compose -f docker-compose.test.yml up -d --build redis gateway router
	@echo "⏳ Waiting for services to be ready..."
	docker-compose -f docker-compose.test.yml exec gateway curl -f http://localhost:8080/health || echo "Gateway not ready yet"

# Run tests against already running services
run-tests:
	@echo "🧪 Running integration tests against running services..."
	docker-compose -f docker-compose.test.yml run --rm test-runner

# Stop and clean up services
test-down:
	@echo "🧹 Cleaning up test environment..."
	docker-compose -f docker-compose.test.yml down -v --remove-orphans

# Show logs from services
test-logs:
	docker-compose -f docker-compose.test.yml logs

# Show logs for specific service
test-logs-gateway:
	docker-compose -f docker-compose.test.yml logs gateway

test-logs-router:
	docker-compose -f docker-compose.test.yml logs router

test-logs-redis:
	docker-compose -f docker-compose.test.yml logs redis

# Build services without running tests (useful for debugging)
build-services:
	@echo "🔨 Building services..."
	docker-compose -f docker-compose.test.yml build

# Run tests in debug mode (services stay up for inspection)
test-debug:
	@echo "🐛 Starting debug mode (services will stay running)..."
	docker-compose -f docker-compose.test.yml up -d --build redis gateway router
	@echo "⏳ Waiting for services to be ready..."
	sleep 15
	@echo "🧪 Running tests..."
	docker-compose -f docker-compose.test.yml run --rm test-runner || true
	@echo "🔍 Services are still running for inspection. Use 'make test-down' to clean up."

# Force rebuild everything
rebuild:
	@echo "🔨 Force rebuilding all services..."
	docker-compose -f docker-compose.test.yml build --no-cache

# Clean everything including images
clean:
	@echo "🧹 Cleaning everything including Docker images..."
	docker-compose -f docker-compose.test.yml down -v --remove-orphans --rmi all

# Health check for running services
health-check:
	@echo "🏥 Checking health of running services..."
	@echo "Redis:" && docker-compose -f docker-compose.test.yml exec redis redis-cli ping || echo "Redis not healthy"
	@echo "Gateway:" && docker-compose -f docker-compose.test.yml exec gateway curl -f http://localhost:8080/health || echo "Gateway not healthy"
	@echo "Router:" && docker-compose -f docker-compose.test.yml exec router curl -f http://localhost:8081/health || echo "Router not healthy"

# Help
help:
	@echo "Integration Test Makefile"
	@echo ""
	@echo "Available targets:"
	@echo "  test            - Run complete integration test suite with cleanup (default)"
	@echo "  test-up         - Start test services only"
	@echo "  test-down       - Stop test services"
	@echo "  run-tests       - Run tests against running services"
	@echo "  test-debug      - Start services and run tests (no cleanup for inspection)"
	@echo "  build-services  - Build services without running tests"
	@echo "  rebuild         - Force rebuild all services"
	@echo "  test-logs       - Show all service logs"
	@echo "  test-logs-*     - Show logs for specific service (gateway, router, redis)"
	@echo "  health-check    - Check health of running services"
	@echo "  clean           - Clean up everything including images"
	@echo "  help            - Show this help"
	@echo ""
	@echo "Examples:"
	@echo "  make test              # Complete test run with cleanup"
	@echo "  make test-debug        # Debug mode - services stay running"
	@echo "  make test-up && make health-check  # Start and verify services"