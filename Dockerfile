FROM golang:alpine AS builder

RUN apk update && \
    apk add git build-base && \
    rm -rf /var/cache/apk/* && \
    mkdir -p "$GOPATH/src/github.com/buildsville/" && \
    git clone https://github.com/buildsville/kube-event-watcher.git && \
    mv kube-event-watcher "$GOPATH/src/github.com/buildsville/" && \
    cd "$GOPATH/src/github.com/buildsville/kube-event-watcher" && \
    GOOS=linux GOARCH=amd64 go build -o /kube-event-watcher

FROM alpine:3.7

COPY --from=builder /kube-event-watcher /bin/kube-event-watcher
RUN mkdir "$HOME/.kube-event-watcher" && \
    touch "$HOME/.kube-event-watcher/config.yaml"

ENTRYPOINT ["/bin/kube-event-watcher"]
