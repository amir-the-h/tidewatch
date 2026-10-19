package main

import (
	"os"

	clicmd "github.com/amir-the-h/tidewatch/cli/cmd"
)

func main() {
	if err := clicmd.Execute(); err != nil {
		os.Exit(1)
	}
}
