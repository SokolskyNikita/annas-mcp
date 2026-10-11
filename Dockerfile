# Container image for the MCP server, for Docker's MCP Catalog and for
# `docker run -i`. CI builds it and checks the MCP handshake with
# scripts/check-docker.mjs.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
# The server reports the embedded internal/version/version.txt.
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/annas-mcp ./cmd/annas-mcp \
    && mkdir /out/downloads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/annas-mcp /usr/local/bin/annas-mcp
# Mount a folder here to keep downloads. The saved session goes to
# /home/nonroot/.cache/annas-mcp unless ANNAS_MCP_CACHE_DIR says otherwise.
COPY --from=build --chown=nonroot:nonroot /out/downloads /downloads
ENV ANNAS_DOWNLOAD_PATH=/downloads
ENTRYPOINT ["/usr/local/bin/annas-mcp"]
CMD ["mcp"]
