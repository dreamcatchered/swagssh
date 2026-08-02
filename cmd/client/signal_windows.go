//go:build windows

package main

import (
	"os"
)

func notifyWinch(ch chan<- os.Signal) {
}
