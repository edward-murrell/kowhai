// Command kowhai is the Kōwhai server and its Claude Code helper.
//
//	kowhai serve           run the server
//	kowhai hook            Claude Code hook (reads event JSON on stdin)
//	kowhai link            link the current project folder to a card
//	kowhai version         print the version
package main

import (
	"fmt"
	"os"
	// Recurring cards need zone data even on images without tzdata.
	_ "time/tzdata"

	"github.com/edward-murrell/kowhai/internal/hook"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "serve":
		err = serve()
	case "hook":
		// The hook must never break Claude Code, so it always exits 0.
		hook.Run(os.Stdin, os.Stdout, os.Args[2:])
		return
	case "link":
		err = hook.Link(os.Args[2:], os.Stdin, os.Stdout)
	case "version", "--version", "-v":
		fmt.Println("kowhai", version)
	case "help", "-h", "--help":
		usage()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "kowhai:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Usage: kowhai <command>

Commands:
  serve     Run the Kōwhai server. Configured with KOWHAI_* environment variables.
  link      Link this project folder to a card. Run "kowhai link -h" for options.
  hook      Report Claude Code session status. Claude Code runs this for you.
  version   Print the version.
`)
}
