FROM eclipse-temurin:25-jdk-noble@sha256:95e350ad1eb738f943e141640f7e3d797e483359a3b33c3864255bfd1ad71f8a AS build
ARG TARGETARCH
RUN apt-get update && apt-get install -y --no-install-recommends unzip build-essential bison flex && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:2c2cd776db3c1f6a097ba418922378ac3649cadd5e844a6f5f088ae628a7be67 https://github.com/NationalSecurityAgency/ghidra/releases/download/Ghidra_12.2_build/ghidra_12.2_PUBLIC_20261016.zip /tmp/ghidra.zip
RUN unzip -q /tmp/ghidra.zip -d /opt && mv /opt/ghidra_* /opt/ghidra && rm /tmp/ghidra.zip
ADD https://github.com/NationalSecurityAgency/ghidra.git#8ca9077040cc43cd790babcfccf19475622a3e9a /src
# Upstream's Makefile defaults to -m32 for non-x86 CPUs. Its public make
# variables select a native build without patching source or using emulation.
RUN if [ "$TARGETARCH" = arm64 ]; then make -C /src/Ghidra/Features/Decompiler/src/decompile/cpp -j2 ghidra_opt ARCH_TYPE= OSDIR=linux_arm_64 && install -D /src/Ghidra/Features/Decompiler/src/decompile/cpp/ghidra_opt /opt/ghidra/Ghidra/Features/Decompiler/os/linux_arm_64/decompile; fi
COPY sample.c /tmp/sample.c
RUN mkdir /samples && gcc -O0 -g0 -DCOUNT=7 /tmp/sample.c -o /samples/one && gcc -O0 -g0 -DCOUNT=11 /tmp/sample.c -o /samples/two

FROM eclipse-temurin:25-jdk-noble@sha256:95e350ad1eb738f943e141640f7e3d797e483359a3b33c3864255bfd1ad71f8a
RUN apt-get update && apt-get install -y --no-install-recommends python3 && rm -rf /var/lib/apt/lists/*
COPY --from=build /opt/ghidra /opt/ghidra
COPY --from=build /samples /samples
WORKDIR /work
ENV MAXMEM=2G PYTHONDONTWRITEBYTECODE=1
ENTRYPOINT ["python3", "/recipe/analyse.py"]
