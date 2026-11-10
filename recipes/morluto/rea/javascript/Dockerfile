FROM node:24-bookworm-slim@sha256:01d566e8d94d8c619591d78652babfccd3c6992a3dffa9ed5ab39e11cf6c70e9
RUN apt-get update && apt-get install -y --no-install-recommends procps ca-certificates && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:8d542ec76a9cb350584b60a6c9c088706ff812d5bb5b094c20c95b6dc25698bb https://registry.npmjs.org/rea-agents/-/rea-agents-6.8.0.tgz /tmp/rea.tgz
RUN npm install --global --omit=dev --ignore-scripts /tmp/rea.tgz && rm /tmp/rea.tgz && npm cache clean --force
WORKDIR /work
ENTRYPOINT ["node", "/recipe/analysis.mjs"]
