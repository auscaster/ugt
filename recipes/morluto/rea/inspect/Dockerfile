FROM node:24-bookworm-slim@sha256:01d566e8d94d8c619591d78652babfccd3c6992a3dffa9ed5ab39e11cf6c70e9 AS node
RUN apt-get update && apt-get install -y --no-install-recommends git build-essential python3 ca-certificates && rm -rf /var/lib/apt/lists/*
ADD https://github.com/morluto/rea.git#cb4e5cfbff25c2dda8683ef8598c17cec8a73473 /src
WORKDIR /src
RUN npm ci --ignore-scripts --no-audit --no-fund && npm run build:cached
# Pack through upstream's own file inventory, retaining its bridges and notices.
RUN npm pack --ignore-scripts --pack-destination /tmp && npm prune --omit=dev --ignore-scripts --no-audit --no-fund && mkdir /runtime && tar -xzf /tmp/rea-agents-*.tgz --strip-components=1 -C /runtime && mv node_modules /runtime/
COPY sample.c /tmp/sample.c
COPY fixtures /tmp/fixtures
RUN mkdir /samples && gcc -shared -fPIC -O0 -g0 -DCOUNT=7 /tmp/sample.c -o /samples/native-one.so && gcc -shared -fPIC -O0 -g0 -DCOUNT=11 /tmp/sample.c -o /samples/native-two.so && cp -a /tmp/fixtures/simple-app /samples/simple-app && npm exec --yes --package=@electron/asar@3.4.1 -- asar pack /samples/simple-app /samples/app.asar
RUN mkdir /tmp/react-build && cd /tmp/react-build && npm install --no-audit --no-fund react@19.2.0 react-dom@19.2.0 esbuild@0.25.11 && cp /tmp/fixtures/app.jsx . && mkdir -p /samples/react-app/assets && ./node_modules/.bin/esbuild app.jsx --bundle --minify --sourcemap --format=esm --legal-comments=linked --outfile=/samples/react-app/assets/app.js && cp /tmp/fixtures/react-package.json /samples/react-app/package.json && cp /tmp/fixtures/index.html /samples/react-app/index.html

# Reuse the Ghidra runtime already qualified on native amd64 and arm64. It
# includes JDK 25 and the matching native arm64 decompiler absent upstream.
FROM ghcr.io/auscaster/ugt/recipes@sha256:987b34bac5943efa9447e58c722510ffd107188128ce963c9dd9791118541072
RUN apt-get update && apt-get install -y --no-install-recommends procps ca-certificates libatomic1 libstdc++6 && rm -rf /var/lib/apt/lists/*
COPY --from=node /usr/local/ /usr/local/
COPY --from=node /runtime /opt/rea
COPY --from=node /samples /samples
RUN ln -s /opt/rea/scripts/rea.mjs /usr/local/bin/rea
ENV GHIDRA_INSTALL_DIR=/opt/ghidra REA_ANALYSIS_PROVIDER=ghidra GHIDRA_HEADLESS_MAXMEM=2G PYTHONDONTWRITEBYTECODE=1
WORKDIR /work
ENTRYPOINT ["python3", "/recipe/analysis.py"]
