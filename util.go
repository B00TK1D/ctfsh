package main

import (
	"log"
	"runtime/debug"
	"math/rand"
)

func must(err error) {
	if err != nil {
		debug.PrintStack()
		log.Fatal(err)
	}
}

func randHex(n int) string {
	const letters = "0123456789abcdef"
	result := make([]byte, n)
	for i := range result {
		result[i] = letters[rand.Intn(len(letters))]
	}
	return string(result)
}
