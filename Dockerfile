FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl git openssh-client && \
    rm -rf /var/lib/apt/lists/*

ARG VERSION=
RUN [ -n "$VERSION" ] || { echo "ERROR: VERSION build arg must be set" >&2; exit 1; }
RUN curl -fsSL https://raw.githubusercontent.com/upsun/cli/$VERSION/installer.sh -o /tmp/installer.sh && \
    INSTALL_METHOD=raw VERSION=$VERSION sh /tmp/installer.sh && \
    rm /tmp/installer.sh

ENTRYPOINT ["upsun"]
