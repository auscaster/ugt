FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.5.1
ADD --checksum=sha256:f9d3e93c499edbe1485bdff14107ca11dded4771a802e81309be75b9683bb9cc https://codeload.github.com/argosopentech/argos-translate/tar.gz/17ed1a9b055c5ba1b51d76b683bbb0c64e2a4a88 /tmp/argos.tar.gz
# The en_es 1.0 package contains a Stanza 1.1 tokenizer and resource index.
RUN pip install --no-cache-dir /tmp/argos.tar.gz stanza==1.1.1 protobuf==3.20.3
ADD --checksum=sha256:d698d0ef87ad70d5d184b7fa6965905bf4368f09a2bb9ffb165a79bac96af0c4 https://argos-net.com/v1/translate-en_es-1_0.argosmodel /tmp/en_es.argosmodel
ENV ARGOS_DEVICE_TYPE=cpu ARGOS_INTRA_THREADS=2
RUN python -c "from pathlib import Path; from argostranslate.package import install_from_path; install_from_path(Path('/tmp/en_es.argosmodel'))"
WORKDIR /work
ENTRYPOINT ["python","check/translate.py"]
