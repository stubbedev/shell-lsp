package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"

	"github.com/stubbedev/shell-lsp/internal/cli"
	"github.com/stubbedev/shell-lsp/internal/jsonrpc"
	"github.com/stubbedev/shell-lsp/internal/server"
)

var version = ""

func main() {
	version = resolvedVersion(version, debug.ReadBuildInfo)
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "shell-lsp: finding the working directory:", err)
		os.Exit(1)
	}
	os.Exit(cli.Run(environment(dir, server.CurrentProcess())))
}

func environment(dir string, process server.Process) cli.Env {
	return cli.Env{
		Args:       os.Args[1:],
		Stdin:      os.Stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		Version:    version,
		Dir:        dir,
		Home:       process.Home,
		ConfigHome: process.ConfigHome,
		PathDirs:   process.PathDirs(),
		Terminal:   terminal(os.Stdout) && os.Getenv("TERM") != "dumb",
		Serve: func() error {
			return server.New(jsonrpc.NewConn(os.Stdin, os.Stdout), version, process).Run(context.Background())
		},
	}
}

func terminal(f *os.File) bool {
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
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
