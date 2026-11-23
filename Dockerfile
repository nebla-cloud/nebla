# syntax=docker/dockerfile:1
# The web bundle is platform-independent: build it natively, never under emulation.
FROM --platform=$BUILDPLATFORM node:26-alpine AS web
# Node 25+ no longer bundles corepack
RUN npm install -g corepack@0.36.0 && corepack enable
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.27 AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/nebla-server ./cmd/nebla-server

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/nebla-server /nebla-server
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/nebla-server", "healthcheck"]
ENTRYPOINT ["/nebla-server"]
