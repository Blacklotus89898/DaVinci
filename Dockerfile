FROM golang:1.24-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /knowledge-service ./cmd/knowledge-service && \
    CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /knowledge ./cmd/knowledge

FROM alpine:3.20

# CA certs for Ollama TLS (if EMBED_URL points to an HTTPS endpoint).
RUN apk --no-cache add ca-certificates

COPY --from=builder /knowledge-service /usr/local/bin/knowledge-service
COPY --from=builder /knowledge /usr/local/bin/knowledge

VOLUME ["/data", "/docs"]

ENV DB_PATH=/data/knowledge.db

EXPOSE 3737

ENTRYPOINT ["knowledge-service"]
