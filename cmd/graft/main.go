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
		// graft init <remote> [repo-path]
		if len(args) < 2 {
			fatalf("usage: graft init <remote> [repo-path]\n")
		}
		repoPath := defaultRepoPath()

		for _, a := range args[2:] {
			if !strings.HasPrefix(a, "-") {
				repoPath = a
			}
		}
		err = commands.Init(args[1], repoPath)

	case "this":
		// graft this <name> [--sudo]
		sudo := false
		blobName := ""
		for _, a := range args[1:] {
			if a == "--sudo" {
				sudo = true
			} else if !strings.HasPrefix(a, "-") {
				blobName = a
			}
		}
		if blobName == "" {
			fatalf("usage: graft this <name> [--sudo]\n")
		}
		err = commands.This(blobName, sudo)

	case "here":
		// graft here [name]: clone existing blob into current directory
		blobName := ""
		if len(args) >= 2 {
			blobName = args[1]
		}
		err = commands.Here(blobName)

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
  init <remote> [repo-path]
        initialize graft repo and config
        base_url is derived from remote automatically
        repo-path defaults to ~/.local/share/graft

  this <name> [--sudo]
        start tracking current directory as blob <name>
        --sudo for root-owned directories

  here [name]
        clone existing blob into current directory
        without name: looks up blob by current path in config

  apply [--force] [name]
        restore blob(s) to paths from config
        --force creates missing directories

  sync  commit and push all blobs, update main repo refs

  pull  pull updates for all blobs

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
