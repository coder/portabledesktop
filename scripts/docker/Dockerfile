FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
    ca-certificates \
    chromium \
    jq \
    file \
    binutils \
    procps \
    psmisc \
    xdg-utils \
 && rm -rf /var/lib/apt/lists/*

COPY pd/dist/portabledesktop /usr/local/bin/portabledesktop
RUN chmod +x /usr/local/bin/portabledesktop

WORKDIR /work
CMD ["bash"]
