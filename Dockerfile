FROM node:24-alpine AS web-builder
WORKDIR /build/web
COPY web/package.json web/package-lock.json* ./
RUN npm ci
COPY api/ ../api/
COPY web/ ./
RUN npm run build

FROM golang:1.27-alpine AS go-builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -o /out/migrate ./cmd/migrate

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=go-builder /out/server /app/server
COPY --from=go-builder /out/migrate /app/migrate
COPY --from=web-builder /build/web/dist /app/web/dist
COPY migrations /app/migrations
COPY config /app/config
COPY prompts /app/prompts
EXPOSE 8080
CMD ["/app/server"]
