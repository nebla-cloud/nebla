# syntax=docker/dockerfile:1
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/nebla-server ./cmd/nebla-server

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /out/nebla-server /nebla-server
USER nonroot:nonroot
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=5s --start-period=20s --retries=3 CMD ["/nebla-server", "healthcheck"]
ENTRYPOINT ["/nebla-server"]
