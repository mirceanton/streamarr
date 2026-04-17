FROM alpine:3.21
RUN apk add --no-cache ffmpeg
USER 8675:8675
WORKDIR /app
COPY streamarr /app/
COPY templates/ /app/templates/
EXPOSE 8080
VOLUME ["/config", "/media"]
ENV STREAMARR_CONFIG_PATH=/config/streamarr.db
ENV STREAMARR_PORT=8080
ENTRYPOINT ["/app/streamarr"]
