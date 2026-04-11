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

	case "push":
		blobName := ""
		for _, a := range args[1:] {
			if !strings.HasPrefix(a, "-") {
				blobName = a
			}
		}
		err = commands.Push(blobName)

	case "pull":
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
		err = commands.Pull(blobName, force)

	case "remove":
		if len(args) < 2 {
			fatalf("usage: graft remove <name>\n")
		}
		err = commands.Remove(args[1])

	case "switch":
		if len(args) < 2 {
			fatalf("usage: graft switch <name>\n")
		}
		err = commands.Switch(args[1])

	case "repo":
		if len(args) < 2 {
			fatalf("usage: graft repo <add|remove|list> [args]\n")
		}
		switch args[1] {
		case "add":
			if len(args) < 3 {
				fatalf("usage: graft repo add <remote>\n")
			}
			err = commands.RepoAdd(args[2])
		case "remove":
			if len(args) < 3 {
				fatalf("usage: graft repo remove <name>\n")
			}
			err = commands.RepoRemove(args[2])
		case "list":
			err = commands.RepoList()
		default:
			fatalf("✗ unknown repo subcommand: %s\n", args[1])
		}

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

  push [name]
        commit and push blob(s), update main repo refs

  pull [--force] [name]
        pull updates for blob(s)
        --force resets to remote HEAD, discarding local changes

  remove <name>
        remove blob from tracking and main repo

  switch <name>
        switch active repo

  repo add <remote>
        add and clone a remote graft repo

  repo remove <name>
        remove a repo from config

  repo list
        list all registered repos
`)
}

func defaultRepoPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".graft"
	}
	return home + "/.local/share/graft"
}
