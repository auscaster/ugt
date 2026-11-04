FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD https://github.com/microsoft/markitdown.git#7482c092d9c0074a89803c9fdb63a969e7fe17fc /src
RUN pip install --no-cache-dir '/src/packages/markitdown[docx,pptx,xlsx]' tabulate
ENV XDG_CACHE_HOME=/tmp/cache
WORKDIR /work
ENTRYPOINT ["markitdown"]
