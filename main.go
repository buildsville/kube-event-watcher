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

	err = validateSlack()
	if err != nil {
		panic(err)
	}

	promServer()
	watchStart(appConf)
}
