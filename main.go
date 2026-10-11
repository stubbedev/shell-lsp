package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/server"
)

var version = ""

func main() {
	version = resolvedVersion(version, debug.ReadBuildInfo)
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

func resolvedVersion(stamped string, buildInfo func() (*debug.BuildInfo, bool)) string {
	if stamped != "" {
		return stamped
	}
	if info, ok := buildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return strings.TrimPrefix(info.Main.Version, "v")
	}
	return "dev"
}

func run() error {
	srv := server.New(jsonrpc.NewConn(os.Stdin, os.Stdout), version)
	return srv.Run(context.Background())
}
