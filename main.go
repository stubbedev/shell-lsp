// Command shell-lsp is a Language Server Protocol server for shell scripts:
// bash, zsh and POSIX sh, with dialect auto-detection.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/server"
)

var version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("shell-lsp " + version)
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "shell-lsp:", err)
		os.Exit(1)
	}
}

func run() error {
	srv := server.New(jsonrpc.NewConn(os.Stdin, os.Stdout), version)
	return srv.Run(context.Background())
}
