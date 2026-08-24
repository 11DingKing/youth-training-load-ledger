FROM golang:1.24-bookworm

WORKDIR /workspace
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go test ./... -count=1 \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /usr/local/bin/youth-load ./cmd/server \
    && useradd --create-home --uid 10001 app \
    && mkdir -p /data \
    && chown app:app /data
VOLUME ["/data"]
ENV HTTP_ADDR=:8080 DATABASE_PATH=/data/youth-load.db
EXPOSE 8080
USER app
ENTRYPOINT ["/usr/local/bin/youth-load"]
