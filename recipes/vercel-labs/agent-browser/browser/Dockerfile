FROM node:24-bookworm-slim@sha256:01d566e8d94d8c619591d78652babfccd3c6992a3dffa9ed5ab39e11cf6c70e9
RUN apt-get update && apt-get install -y --no-install-recommends chromium fonts-liberation ca-certificates && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:3c2546296646b858a29da3c0a2f0f891c322ab32941c84b251f63c0dd803dd98 https://registry.npmjs.org/agent-browser/-/agent-browser-0.42.0.tgz /tmp/browser.tgz
RUN npm install --global --omit=dev /tmp/browser.tgz && rm /tmp/browser.tgz && npm cache clean --force
ENV AGENT_BROWSER_EXECUTABLE_PATH=/usr/bin/chromium AGENT_BROWSER_ARGS=--no-sandbox,--disable-dev-shm-usage AGENT_BROWSER_DEFAULT_TIMEOUT=10000
WORKDIR /work
ENTRYPOINT ["node", "/recipe/browser.mjs"]
