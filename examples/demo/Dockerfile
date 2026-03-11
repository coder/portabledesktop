FROM node:20-bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PORT=5190

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates dumb-init chromium \
  && rm -rf /var/lib/apt/lists/*

# Copy the pre-built portabledesktop Go binary.
COPY pd/dist/portabledesktop /usr/local/bin/portabledesktop

# Copy the pre-built demo bundle (produced by `bun build` locally).
COPY examples/demo/dist/index.js /app/index.js

EXPOSE 5190

ENTRYPOINT ["dumb-init", "--", "node", "/app/index.js"]
CMD []
