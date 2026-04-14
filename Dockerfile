FROM alpine:3

RUN apk add --no-cache ca-certificates curl git openssh-client

ARG VERSION=
RUN [ -n "$VERSION" ] || { echo "VERSION is required" >&2; exit 1; }
COPY installer.sh /tmp/installer.sh
RUN INSTALL_METHOD=raw VERSION=$VERSION sh /tmp/installer.sh && rm /tmp/installer.sh

ENTRYPOINT ["upsun"]