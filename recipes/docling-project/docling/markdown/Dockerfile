FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends libgl1 libglib2.0-0 && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.8.0 torchvision==0.23.0 --index-url https://download.pytorch.org/whl/cpu
ADD --checksum=sha256:4e0f63c4bd73a3ceb39bbfdb7abf946ae27338c372279e27b884fd82d38c2b5c https://files.pythonhosted.org/packages/14/fd/2efbed180895a369d899ad1b09b4931715179494953453906d7a1b1f10fa/docling_slim-2.146.0.tar.gz /tmp/docling_slim-2.146.0.tar.gz
RUN pip install --no-cache-dir 'docling-slim[convert-core,format-pdf,models-local] @ file:///tmp/docling_slim-2.146.0.tar.gz' reportlab
RUN python -c "from huggingface_hub import snapshot_download as get; get('docling-project/docling-layout-heron',revision='8f39ad3c0b4c58e9c2d2c84a38465abf757272d8',local_dir='/models/docling-project--docling-layout-heron',allow_patterns=['*.json','model.safetensors','README.md','LICENSE*']); get('docling-project/docling-models',revision='2199320848bb9a8a519d22e4b528185a4f9a6f64',local_dir='/models/docling-project--docling-models',allow_patterns=['model_artifacts/tableformer/accurate/*','README.md','LICENSE*'])"
# The local TableFormer backend imports cv2 when the first PDF is opened.
RUN pip install --no-cache-dir opencv-python-headless==4.11.0.86
COPY CDLA-Permissive-2.0.txt MODEL-NOTICE /models/
ENV HF_HUB_OFFLINE=1 OMP_NUM_THREADS=2 TOKENIZERS_PARALLELISM=false
WORKDIR /work
ENTRYPOINT ["python","check/convert.py"]
