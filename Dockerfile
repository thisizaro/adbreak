# Build the SPA, compile the Go binary with the SPA embedded, ship on Debian
# slim with ffmpeg (drawtext) and fonts. Not distroless: the pipeline shells out to ffmpeg.
FROM node:24-trixie-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.27-trixie AS build
WORKDIR /src
COPY go.mod ./
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/adbreak ./cmd/server

FROM debian:trixie-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg fonts-dejavu-core ca-certificates \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/adbreak /usr/local/bin/adbreak
RUN mkdir -p /data /videos && chown nobody:nogroup /data /videos
ENV PORT=8080 DATA_DIR=/data VIDEO_DIR=/videos BRANDS_PATH=/videos/brands.json
EXPOSE 8080
USER nobody
ENTRYPOINT ["adbreak"]
