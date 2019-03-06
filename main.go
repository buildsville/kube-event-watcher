package main

import (
	"flag"
	"os"

	"github.com/buildsville/kube-event-watcher/watcher"
	"github.com/golang/glog"
)

var (
	appVersion = "latest"
	version    = flag.Bool("version", false, "prints current version.")
)

func main() {
	flag.Parse()

	if *version {
		glog.Infof("version: %v\n", appVersion)
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
