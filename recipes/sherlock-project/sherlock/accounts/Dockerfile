FROM python:3.11-slim-bookworm@sha256:94a8fee35252d06ab75856f15f12c7bae67b10e8a6da93c7fded82b2610926cd
ADD https://github.com/sherlock-project/sherlock.git#c588d7c8b476df9f9c84912a05d1430fb8cdcdae /src
RUN pip install --no-cache-dir /src
ENV PYTHONDONTWRITEBYTECODE=1
WORKDIR /work
ENTRYPOINT ["python", "/recipe/accounts.py"]
