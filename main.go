package main

import (
	"fmt"
	"os"

	"github.com/McKean/aiquokka/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "aiquokka: "+err.Error())
		os.Exit(1)
	}
}
