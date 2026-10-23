FROM node:24.22.1-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27.1-bookworm AS backend
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates libstdc++6 python3 python3-venv \
    && python3 -m venv /opt/yt-dlp \
    && /opt/yt-dlp/bin/pip install --no-cache-dir 'yt-dlp[default]==2026.6.9' \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home app
COPY --from=web /usr/local/bin/node /usr/local/bin/node
COPY --from=backend /out/server /app/server
COPY --from=web /src/web/dist /app/web/dist
ENV PATH="/opt/yt-dlp/bin:${PATH}" HTTP_ADDR=0.0.0.0:8080 WEB_DIST=/app/web/dist
WORKDIR /app
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
