package main

import (
	"flag"
)

func main() {
	flag.Parse()

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
