package main

import (
	"fmt"
	"os"

	"github.com/agorf/thyme-backend/photos"
	"github.com/agorf/thyme-backend/server"
	"github.com/agorf/thyme-backend/thumbs"
)

const helpText = `NAME:
    thyme - browse and view your photos

USAGE:
    thyme command [arguments...]

COMMANDS:
    scan   <path>...  import photo metadata into database
    thumbs <path>     generate photo thumbs (under <path>/public/thumbs)
    run    [<path>]   run web server (rooted at <path>/public)
`

func main() {
	var cmd string
	var args []string

	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	if len(os.Args) > 2 {
		args = os.Args[2:]
	}

	switch cmd {
	case "scan":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "no paths specified")
			os.Exit(1)
		}
		photos.Scan(args...)
	case "thumbs":
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "no path specified")
			os.Exit(1)
		}
		thumbs.Generate(args[0])
	case "run":
		thymePath := "."
		if len(args) > 0 {
			thymePath = args[0]
		}
		server.Serve(thymePath)
	default:
		fmt.Println(helpText)
	}
}
