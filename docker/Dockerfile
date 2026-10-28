# Loadsmart image, built by CircleCI with lumper (see .circleci/config.yml).
# lumper uses the classic Docker builder, so this file avoids the BuildKit-only
# features of the upstream ./Dockerfile (RUN --mount, --platform=$BUILDPLATFORM).
FROM golang:1.25.9-alpine AS builder

ARG TARGETARCH=amd64
ARG RELEASE=dev

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -ldflags="-w -s -X main.Version=${RELEASE}" -trimpath -o trino-mcp ./cmd/

FROM alpine:3.22.2
RUN apk --no-cache add ca-certificates

WORKDIR /app

COPY --from=builder /app/trino-mcp .

ENV TRINO_HOST="trino" \
    TRINO_PORT="8080" \
    TRINO_USER="trino" \
    TRINO_CATALOG="memory" \
    TRINO_SCHEMA="default" \
    MCP_TRANSPORT="http" \
    MCP_HOST="0.0.0.0" \
    MCP_PORT="8080"

EXPOSE 8080

CMD ["./trino-mcp"]
