FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
RUN apt-get update && apt-get install -y --no-install-recommends espeak-ng && rm -rf /var/lib/apt/lists/*
RUN pip install --no-cache-dir torch==2.5.1 --index-url https://download.pytorch.org/whl/cpu || pip install --no-cache-dir torch==2.5.1
ADD --checksum=sha256:fbf633262797f8cf46fdac3315cf9cade67dc8b762c0feccf334892772fb9ac4 https://files.pythonhosted.org/packages/e8/48/88b8cdf28b068d070195c2817175549dee48e7682e3ab8994bee5f69217e/kokoro-0.9.4.tar.gz /tmp/kokoro-0.9.4.tar.gz
RUN pip install --no-cache-dir /tmp/kokoro-0.9.4.tar.gz 'transformers>=4.46,<4.50' 'spacy>=3.8,<3.9' 'https://github.com/explosion/spacy-models/releases/download/en_core_web_sm-3.8.0/en_core_web_sm-3.8.0-py3-none-any.whl'
ADD https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/config.json /models/config.json
ADD --checksum=sha256:496dba118d1a58f5f3db2efc88dbdc216e0483fc89fe6e47ee1f2c53f18ad1e4 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/kokoro-v1_0.pth /models/kokoro.pth
ADD --checksum=sha256:6d877149dd8b348fbad12e5845b7e43d975390e9f3b68a811d1d86168bef5aa3 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_alloy.pt /models/voices/af_alloy.pt
ADD --checksum=sha256:c03bd1a4c3716c2d8eaa3d50022f62d5c31cfbd6e15933a00b17fefe13841cc4 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_aoede.pt /models/voices/af_aoede.pt
ADD --checksum=sha256:8cb64e02fcc8de0327a8e13817e49c76c945ecf0052ceac97d3081480e8e48d6 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_bella.pt /models/voices/af_bella.pt
ADD --checksum=sha256:0ab5709b8ffab19bfd849cd11d98f75b60af7733253ad0d67b12382a102cb4ff https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_heart.pt /models/voices/af_heart.pt
ADD --checksum=sha256:cdfdccb8cc975aa34ee6b89642963b0064237675de0e41a30ae64cc958dd4e87 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_jessica.pt /models/voices/af_jessica.pt
ADD --checksum=sha256:8bfbc512321c3db49dff984ac675fa5ac7eaed5a96cc31104d3a9080e179d69d https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_kore.pt /models/voices/af_kore.pt
ADD --checksum=sha256:c5561808bcf5250fe8c5f5de32caf2d94f27e57e95befdb098c5c85991d4c5da https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_nicole.pt /models/voices/af_nicole.pt
ADD --checksum=sha256:e0233676ddc21908c37a1f102f6b88a59e4e5c1bd764983616eb9eda629dbcd2 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_nova.pt /models/voices/af_nova.pt
ADD --checksum=sha256:e149459bd9c084416b74756b9bd3418256a8b839088abb07d463730c369dab8f https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_river.pt /models/voices/af_river.pt
ADD --checksum=sha256:49bd364ea3be9eb3e9685e8f9a15448c4883112a7c0ff7ab139fa4088b08cef9 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_sarah.pt /models/voices/af_sarah.pt
ADD --checksum=sha256:c799548aed06e0cb0d655a85a01b48e7f10484d71663f9a3045a5b9362e8512c https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/af_sky.pt /models/voices/af_sky.pt
ADD --checksum=sha256:ced7e284aba12472891be1da3ab34db84cc05cc02b5889535796dbf2d8b0cb34 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_adam.pt /models/voices/am_adam.pt
ADD --checksum=sha256:8bcfdc852bc985fb45c396c561e571ffb9183930071f962f1b50df5c97b161e8 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_echo.pt /models/voices/am_echo.pt
ADD --checksum=sha256:ada66f0eefff34ec921b1d7474d7ac8bec00cd863c170f1c534916e9b8212aae https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_eric.pt /models/voices/am_eric.pt
ADD --checksum=sha256:98e507eca1db08230ae3b6232d59c10aec9630022d19accac4f5d12fcec3c37a https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_fenrir.pt /models/voices/am_fenrir.pt
ADD --checksum=sha256:c82550757ddb31308b97f30040dda8c2d609a9e2de6135848d0a948368138518 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_liam.pt /models/voices/am_liam.pt
ADD --checksum=sha256:9a443b79a4b22489a5b0ab7c651a0bcd1a30bef675c28333f06971abbd47bd37 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_michael.pt /models/voices/am_michael.pt
ADD --checksum=sha256:e8452be16cd0f6da7b4579eaf7b1e4506e92524882053d86d72b96b9a7fed584 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_onyx.pt /models/voices/am_onyx.pt
ADD --checksum=sha256:dd1d8973f4ce4b7d8ae407c77a435f485dabc052081b80ea75c4f30b84f36223 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_puck.pt /models/voices/am_puck.pt
ADD --checksum=sha256:7f2f7582fa2b1f160e90aafe6d0b442a685e773608b6667e545d743b073e97a7 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/am_santa.pt /models/voices/am_santa.pt
ADD --checksum=sha256:d292651b6af6c0d81705c2580dcb4463fccc0ff7b8d618a471dbb4e45655b3f3 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bf_alice.pt /models/voices/bf_alice.pt
ADD --checksum=sha256:d0a423deabf4a52b4f49318c51742c54e21bb89bbbe9a12141e7758ddb5da701 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bf_emma.pt /models/voices/bf_emma.pt
ADD --checksum=sha256:cdd4c37003805104d1d08fb1e05855c8fb2c68de24ca6e71f264a30aaa59eefd https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bf_isabella.pt /models/voices/bf_isabella.pt
ADD --checksum=sha256:6e09c2e481e2d53004d7e5ae7d3a325369e130a6f45c35a6002de75084be9285 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bf_lily.pt /models/voices/bf_lily.pt
ADD --checksum=sha256:fc3fce4e9c12ed4dbc8fa9680cfe51ee190a96444ce7c3ad647549a30823fc5d https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bm_daniel.pt /models/voices/bm_daniel.pt
ADD --checksum=sha256:d44935f3135257a9064df99f007fc1342ff1aa767552b4a4fa4c3b2e6e59079c https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bm_fable.pt /models/voices/bm_fable.pt
ADD --checksum=sha256:f1bc812213dc59774769e5c80004b13eeb79bd78130b11b2d7f934542dab811b https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bm_george.pt /models/voices/bm_george.pt
ADD --checksum=sha256:b5204750dcba01029d2ac9cec17aec3b20a6d64073c579d694a23cb40effbd0e https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/voices/bm_lewis.pt /models/voices/bm_lewis.pt
ADD --checksum=sha256:91dcabced89db6f109b8786642f50402d3ee87450e8189589b6f85520e7f4d78 https://huggingface.co/hexgrad/Kokoro-82M/resolve/f3ff3571791e39611d31c381e3a41a3af07b4987/README.md /models/MODEL-CARD.md
RUN cp /usr/share/common-licenses/Apache-2.0 /models/LICENSE
ENV HF_HUB_OFFLINE=1 OMP_NUM_THREADS=2
WORKDIR /work
ENTRYPOINT ["python","check/speak.py"]
