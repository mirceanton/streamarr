FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o streamarr .

FROM alpine:3.20
RUN apk add --no-cache ffmpeg
WORKDIR /app
COPY --from=builder /app/streamarr .
COPY templates/ templates/
EXPOSE 8080
VOLUME ["/config", "/media"]
ENV STREAMARR_CONFIG_PATH=/config/streamarr.db
ENV STREAMARR_PORT=8080
CMD ["./streamarr"]
