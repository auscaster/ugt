FROM oven/bun:1.4.4@sha256:1d22548f177f4ff38e0c15baa3c2c46c6c2060ba2d1bc573bb4720b407548f1d
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates && rm -rf /var/lib/apt/lists/*
ENV GBRAIN_NO_AUTOPILOT_INSTALL=1 GBRAIN_NO_REEMBED=1 GBRAIN_HOME=/state
ADD https://github.com/garrytan/gbrain.git#a94d50f3584d84cdb5966f43cca21a877f209286 /app
WORKDIR /app
RUN bun install --frozen-lockfile
ENTRYPOINT ["bun", "/app/src/cli.ts"]
