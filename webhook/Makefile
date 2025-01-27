VERSION ?= v0.0.1
REGISTRY ?= fengyu225
NAME ?= cacerts-webhook
IMAGE_TAG ?= $(VERSION)
PLATFORMS ?= linux/amd64

.PHONY: init
init:
	@echo "Initializing Go modules..."
	@if [ ! -f go.mod ]; then \
		go mod init webhook; \
	fi
	go mod tidy

.PHONY: build
build: init
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/webhook

.PHONY: docker-build
docker-build:
	docker build --platform linux/amd64 -t $(REGISTRY)/$(NAME):$(IMAGE_TAG) -f Dockerfile .

.PHONY: docker-push
docker-push:
	docker push $(REGISTRY)/$(NAME):$(IMAGE_TAG)

.PHONY: clean
clean:
	rm -f bin/webhook

.PHONY: clean-deps
clean-deps:
	rm -f go.sum
	rm -f go.mod
