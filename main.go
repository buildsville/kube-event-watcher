package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/buildsville/kube-event-watcher/watcher"
)

var (
	appVersion = "latest"
	version    = flag.Bool("version", false, "prints current version.")
)

func main() {
	flag.Parse()

	if *version {
		fmt.Printf("kube-event-watcher version: %v\n", appVersion)
		os.Exit(0)
	}

	appConf, err := watcher.LoadConfig()
	if err != nil {
		panic(err)
	}

	if e := watcher.ValidateSlack(); e != nil {
		panic(e)
	}

	if e := watcher.ValidateCWLogs(); e != nil {
		panic(e)
	}

	watcher.PromServer()
	watcher.WatchStart(appConf)
}
