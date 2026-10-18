// Command bblv is the Bambu Live View server.
package main

import (
	"fmt"
	"os"
)

const usage = `Usage: bblv <command> [flags]

Commands:
  record     connect to a printer and record its MQTT reports to a .jsonl file
  replay     play back a recording and log the printer status
  geometry   build the 3D geometry of the current print (or of a local file)

Run "bblv <command> -h" for command flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "record":
		err = runRecord(os.Args[2:])
	case "replay":
		err = runReplay(os.Args[2:])
	case "geometry":
		err = runGeometry(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
