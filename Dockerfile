FROM alpine:3.21

RUN apk add --no-cache ffmpeg
USER 1000:1000

WORKDIR /app
COPY streamarr /app/
COPY templates/ /app/templates/

ENV STREAMARR_CONFIG_PATH=/config/streamarr.db
ENV STREAMARR_PORT=8080

ENTRYPOINT ["/app/streamarr"]
