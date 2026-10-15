IMAGE    ?= go-admin-mysql
TAG      ?= latest
REGISTRY ?= your-registry

.PHONY: help deps build run dev docker-build docker-push k8s-deploy k8s-delete lint

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

deps: ## Download and tidy Go modules
	go mod tidy

build: ## Compile the binary
	go build -o go-admin-mysql .

dev: ## Generate random credentials and run locally (development only)
	$(eval ADMIN_PASS    := $(shell openssl rand -base64 24))
	$(eval ENCRYPTION_KEY := $(shell openssl rand -hex 32))
	@echo "---"
	@echo "Dev credentials (not persisted):"
	@echo "  ADMIN_USER=admin"
	@echo "  ADMIN_PASS=$(ADMIN_PASS)"
	@echo "---"
	@ADMIN_USER=admin \
	 ADMIN_PASS="$(ADMIN_PASS)" \
	 ENCRYPTION_KEY="$(ENCRYPTION_KEY)" \
	 DATA_DIR=./data \
	 PORT=8080 \
	 go run .

run: ## Run with credentials from environment (ADMIN_PASS and ENCRYPTION_KEY required)
	@if [ -z "$$ADMIN_PASS" ] || [ -z "$$ENCRYPTION_KEY" ]; then \
		echo "ERROR: ADMIN_PASS and ENCRYPTION_KEY must be set."; \
		echo "Generate them with:"; \
		echo "  export ADMIN_PASS=\$$(openssl rand -base64 24)"; \
		echo "  export ENCRYPTION_KEY=\$$(openssl rand -hex 32)"; \
		exit 1; \
	fi
	ADMIN_USER=$${ADMIN_USER:-admin} DATA_DIR=./data PORT=8080 go run .

docker-build: ## Build Docker image
	docker build -t $(REGISTRY)/$(IMAGE):$(TAG) .

docker-push: ## Push Docker image to registry
	docker push $(REGISTRY)/$(IMAGE):$(TAG)

k8s-deploy: ## Apply all Kubernetes manifests
	kubectl apply -f k8s/namespace.yaml
	kubectl apply -f k8s/pvc.yaml
	kubectl apply -f k8s/secret.yaml
	kubectl apply -f k8s/deployment.yaml
	kubectl apply -f k8s/service.yaml
	kubectl apply -f k8s/ingress.yaml

k8s-delete: ## Remove the mysql-admin namespace and all resources
	kubectl delete namespace mysql-admin
