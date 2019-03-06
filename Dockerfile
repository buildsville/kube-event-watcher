FROM golang:alpine AS builder

ADD ./ /tmp/kube-event-watcher/

RUN apk update && \
    apk add git build-base && \
    rm -rf /var/cache/apk/* && \
    mkdir -p "$GOPATH/src/github.com/buildsville/" && \
    mv /tmp/kube-event-watcher/examples/default.yaml /config.yaml && \
    mv /tmp/kube-event-watcher "$GOPATH/src/github.com/buildsville/" && \
    cd "$GOPATH/src/github.com/buildsville/kube-event-watcher" && \
    make build-linux && \
    mv bin/kube-event-watcher /kube-event-watcher

FROM alpine:3.7

RUN apk add --update ca-certificates && \
    mkdir /root/.kube-event-watcher

COPY --from=builder /kube-event-watcher /kube-event-watcher
COPY --from=builder /config.yaml /root/.kube-event-watcher/config.yaml

ENTRYPOINT ["/kube-event-watcher","-logtostderr"]
