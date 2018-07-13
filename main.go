package main

import (
//"fmt"
)

func main() {
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
