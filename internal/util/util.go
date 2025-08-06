package util

import (
	"log"
	"math/rand"
	"runtime/debug"
)

// Must panics if the error is not nil
func Must(err error) {
	if err != nil {
		debug.PrintStack()
		log.Fatal(err)
	}
}

// RandHex generates a random hexadecimal string of length n
func RandHex(n int) string {
	const letters = "0123456789abcdef"
	result := make([]byte, n)
	for i := range result {
		result[i] = letters[rand.Intn(len(letters))]
	}
	return string(result)
}
