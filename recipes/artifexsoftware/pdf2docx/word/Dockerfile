FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD --checksum=sha256:a293e9e78d89b12a4a43fcefba1346de220681c3daf20b8a7d3e1fce77f0fe97 https://files.pythonhosted.org/packages/71/4d/4041fddff079a2cd0c87612afe74a16d2cf032b2aa73ecddbc6bca2a04fe/pdf2docx-0.5.13-py3-none-any.whl /tmp/pdf2docx-0.5.13-py3-none-any.whl
RUN pip install --no-cache-dir --only-binary=:all: /tmp/pdf2docx-*.whl PyMuPDF==1.26.7
ADD --checksum=sha256:71add8bdc8eb1aaa207c69a13400693f06ad9b927bea976f5d5ab9df0bb489c3 https://files.pythonhosted.org/packages/48/d6/09b28f027b510838559f7748807192149c419b30cb90e6d5f0cf916dc9dc/pymupdf-1.26.7.tar.gz /usr/share/doc/pymupdf/pymupdf-1.26.7.tar.gz
WORKDIR /work
ENTRYPOINT ["python","check/task.py"]
