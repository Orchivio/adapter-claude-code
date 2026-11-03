IMAGE ?= localhost/orchivio-sandbox-claude:dev

.PHONY: test image
test:
	go test ./...

image:
	GOOS=linux GOARCH=$(shell podman info --format '{{.Host.Arch}}') CGO_ENABLED=0 GOWORK=off go build -o build/orchivio-claude-adapter ./cmd/orchivio-claude-adapter
	podman build -t $(IMAGE) .
