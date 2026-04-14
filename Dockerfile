FROM ubuntu:24.04

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates curl git openssh-client && \
    rm -rf /var/lib/apt/lists/*

ARG VERSION=
RUN [ -n "$VERSION" ] || { echo "ERROR: VERSION build arg must be set" >&2; exit 1; }
COPY installer.sh /tmp/installer.sh
RUN INSTALL_METHOD=raw VERSION=$VERSION sh /tmp/installer.sh && \
    rm /tmp/installer.sh

ENTRYPOINT ["upsun"]
