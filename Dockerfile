# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /deepwiki-open-mcp .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /deepwiki-open-mcp /deepwiki-open-mcp
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/deepwiki-open-mcp"]
