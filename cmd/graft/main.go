package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/mcbalaam/graft/internal/commands"
)

var version = "dev"

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(0)
	}

	var err error
	switch args[0] {
	case "init":
		// graft init <remote> <base-url> [repo-path]
		if len(args) < 3 {
			fatalf("usage: graft init <remote> <base-url> [repo-path]\n")
		}
		repoPath := defaultRepoPath()
		if len(args) >= 4 {
			repoPath = args[3]
		}
		err = commands.Init(args[1], args[2], repoPath)

	case "here":
		// graft here <name>
		if len(args) < 2 {
			fatalf("usage: graft here <name>\n")
		}
		err = commands.Here(args[1])

	case "this":
		// graft this [name]
		blobName := ""
		if len(args) >= 2 {
			blobName = args[1]
		}
		err = commands.This(blobName)

	case "apply":
		// graft apply [--force] [name]
		force := false
		blobName := ""
		for _, a := range args[1:] {
			switch {
			case a == "--force":
				force = true
			case !strings.HasPrefix(a, "-"):
				blobName = a
			}
		}
		err = commands.Apply(blobName, force)

	case "sync":
		err = commands.Sync()

	case "pull":
		err = commands.Pull()

	case "remove":
		if len(args) < 2 {
			fatalf("usage: graft remove <name>\n")
		}
		err = commands.Remove(args[1])

	case "version", "--version", "-v":
		fmt.Println("graft", version)
		return

	default:
		fmt.Fprintf(os.Stderr, "✗ unknown command: %s\n\n", args[0])
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format, a...)
	os.Exit(1)
}

func usage() {
	fmt.Printf("graft %s: a Git backup utility\nusage: graft <command> [args]\n", version)
	fmt.Print(`
commands:
  init <remote> <base-url> [repo-path]
        initialise graft repo and config
        repo-path defaults to ~/.local/share/graft

  here <name>
        start tracking current directory as blob <name>

  this [name]
        clone existing blob into current directory
        without name: looks up blob by current path in config

  apply [--force] [name]
        restore blob(s) to paths from config
        --force creates missing directories

  sync  commit and push all blobs, update main repo refs

  pull  pull updates for all blobs (interactive conflict resolution)

  remove <name>
        remove blob from tracking and main repo
`)
}

func defaultRepoPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".graft"
	}
	return home + "/.local/share/graft"
}
