package main

import (
	"flag"
	"github.com/golang/glog"
	"os"
)

const (
	appVersion = "v0.4"
)

var (
	version = flag.Bool("version", false, "prints current version.")
)

func main() {
	flag.Parse()

	if *version {
		glog.Infof("version: %v\n", appVersion)
		os.Exit(0)
	}

	appConf, err := loadConfig()
	if err != nil {
		panic(err)
	}

	if e := validateSlack(); e != nil {
		panic(e)
	}

	if e := validateCWLogs(); e != nil {
		panic(e)
	}

	promServer()
	watchStart(appConf)
}
