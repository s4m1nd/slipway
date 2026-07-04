package main

import (
	"os"

	"github.com/s4m1nd/slipway/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
