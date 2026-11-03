# Single stage: the adapter binary is cross-compiled on the host (see `make image`)
# because the contract module lives outside this build context.
FROM node:22-slim

RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*

RUN npm install -g @anthropic-ai/claude-code

COPY build/orchivio-claude-adapter /usr/local/bin/orchivio-claude-adapter

ENV CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1 \
    DISABLE_AUTOUPDATER=1

# --permission-mode bypassPermissions refuses to run as root
USER node
WORKDIR /work

ENTRYPOINT ["orchivio-claude-adapter","run"]
