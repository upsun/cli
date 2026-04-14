FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends curl bash git ssh-client && \
    rm -rf /var/lib/apt/lists/*

ARG VERSION=
RUN curl -fsSL https://raw.githubusercontent.com/upsun/cli/$VERSION/installer.sh | INSTALL_METHOD=raw VERSION=$VERSION bash

ENTRYPOINT ["upsun"]
