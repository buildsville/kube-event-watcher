package main

import (
	"flag"
)

func main() {
	flag.Parse()

	err := validateConfig()
	if err != nil {
		panic(err)
	}

	err = validateSlack()
	if err != nil {
		panic(err)
	}

	promServer()
	watchStart()
}
