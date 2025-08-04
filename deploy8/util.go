package main

import (
	"log"
	"runtime/debug"
)

func must(err error) {
	if err != nil {
		debug.PrintStack()
		log.Fatal(err)
	}
}
