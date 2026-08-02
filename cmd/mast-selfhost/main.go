package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: mast-selfhost <init|serve|pairing|version>")
	}
	switch args[0] {
	case "init":
		return runInit(args[1:])
	case "serve":
		return runServe(args[1:])
	case "pairing":
		return runPairing(args[1:])
	case "version":
		fmt.Println(versionString())
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}
