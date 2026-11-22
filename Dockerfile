# syntax=docker/dockerfile:1
# Cross-compiles on the build platform; no emulation needed for multi-arch.
FROM --platform=$BUILDPLATFORM golang:1.26.3-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/orchivio-claude-adapter ./cmd/orchivio-claude-adapter

FROM node:22-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
ARG CLAUDE_CODE_VERSION=latest
RUN npm install -g @anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}
COPY --from=build /out/orchivio-claude-adapter /usr/local/bin/orchivio-claude-adapter
ENV CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
    DISABLE_AUTOUPDATER=1
# --permission-mode bypassPermissions refuses to run as root
USER node
WORKDIR /work
ENTRYPOINT ["orchivio-claude-adapter","run"]
