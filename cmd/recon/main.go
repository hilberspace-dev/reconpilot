package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: recon <ingest|run|report>")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ingest", "run", "report":
		fmt.Fprintln(os.Stderr, os.Args[1]+": not implemented yet")
		os.Exit(1)
	default:
		fmt.Fprintln(os.Stderr, "unknown command: "+os.Args[1])
		os.Exit(2)
	}
}
