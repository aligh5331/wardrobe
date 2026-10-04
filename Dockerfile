# syntax=docker/dockerfile:1

# UI: built once on the build machine's platform; the output is plain files.
FROM --platform=$BUILDPLATFORM node:22-alpine AS ui
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# Go: pure-Go SQLite, so cross-compiling needs no C toolchain.
FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=ui /src/frontend/dist ./frontend/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      go build -trimpath -ldflags="-s -w" -o /out/bin/wardrobe ./cmd/server && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
      go build -trimpath -ldflags="-s -w" -o /out/bin/ingest ./cmd/ingest && \
    mkdir -p /out/app/data /out/app/logs

# Runtime: no shell, non-root, CA certificates for Open-Meteo over HTTPS.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build --chown=nonroot:nonroot /out/app /app
COPY --from=build /out/bin/ /usr/local/bin/
ENV GIN_MODE=release
EXPOSE 8080
VOLUME ["/app/data", "/app/logs"]
ENTRYPOINT ["/usr/local/bin/wardrobe"]
