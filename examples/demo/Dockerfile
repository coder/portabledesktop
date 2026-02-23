FROM node:20-bookworm-slim
ENV DEBIAN_FRONTEND=noninteractive
ENV PORT=5190

WORKDIR /app

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates dumb-init chromium \
  && rm -rf /var/lib/apt/lists/*

COPY examples/demo/index.mjs /app/index.mjs
COPY scripts/wallpaper.jpg /app/wallpaper.jpg
COPY portabledesktop.tgz /app/portabledesktop.tgz

RUN npm install --omit=dev --no-fund --no-audit \
  "/app/portabledesktop.tgz" \
  "ai" \
  "@ai-sdk/anthropic" \
  "ws"

EXPOSE 5190

ENTRYPOINT ["dumb-init", "--", "node", "/app/index.mjs"]
CMD []
