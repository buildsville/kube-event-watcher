COMMIT=$(shell git show -s --format=%H)
TAG=$(shell git describe --exact-match --tags $(COMMIT) 2>/dev/null)
DEFAULT_VERSION=latest
VERSION=$(shell make version)
OUTPUT_PATH=bin/kube-event-watcher

build:
	go build -ldflags "-X main.appVersion=$(VERSION)" -o $(OUTPUT_PATH)

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags "-X main.appVersion=$(VERSION)" -o $(OUTPUT_PATH)

build-darwin:
	GOOS=darwin GOARCH=amd64 go build -ldflags "-X main.appVersion=$(VERSION)" -o $(OUTPUT_PATH)

version:
	@if [ -z $(TAG) ] ; then \
		echo $(DEFAULT_VERSION); \
	else \
		echo $(TAG); \
	fi
