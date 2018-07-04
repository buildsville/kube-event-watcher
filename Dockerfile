FROM golang:alpine

RUN apk update && \
    apk add git build-base ca-certificates && \
    rm -rf /var/cache/apk/* && \
    mkdir /root/.kube-event-watcher && \
    mkdir -p "$GOPATH/src/github.com/buildsville/" && \
    git clone https://github.com/buildsville/kube-event-watcher.git && \
    mv kube-event-watcher/examples/default.yaml /config.yaml && \
    mv kube-event-watcher "$GOPATH/src/github.com/buildsville/" && \
    cd "$GOPATH/src/github.com/buildsville/kube-event-watcher" && \
    GOOS=linux GOARCH=amd64 go build -o /kube-event-watcher && \
    cp /kube-event-watcher /bin/kube-event-watcher && \
    cp /config.yaml /root/.kube-event-watcher/config.yaml

ENTRYPOINT ["/bin/kube-event-watcher"]
