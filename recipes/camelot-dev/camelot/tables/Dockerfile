FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD --checksum=sha256:e96891d30707b3e2f1c08cb7ae15eea67abe72485ca92c7025b6308ba37bd3cd https://files.pythonhosted.org/packages/4d/6c/3b6057077feb802ce6ef4e2ebd11a50c12b7065aeb946cc0d9296d195246/camelot_py-2.0.0.tar.gz /tmp/camelot_py-2.0.0.tar.gz
RUN pip install --no-cache-dir /tmp/camelot_py-2.0.0.tar.gz reportlab
WORKDIR /work
ENTRYPOINT ["python","check/extract.py"]
