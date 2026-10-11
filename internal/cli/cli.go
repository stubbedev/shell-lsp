package cli

import (
	"fmt"
	"io"
)

type Env struct {
	Args       []string
	Stdin      io.Reader
	Stdout     io.Writer
	Stderr     io.Writer
	Version    string
	Dir        string
	Home       string
	ConfigHome string
	PathDirs   []string
	Terminal   bool
	Serve      func() error
}

const (
	statusOK      = 0
	statusFailed  = 1
	statusUsage   = 2
	programName   = "shell-lsp"
	versionPrefix = programName + " "
)

const usage = `usage: shell-lsp [command] [arguments]

Commands:
  serve        speak the Language Server Protocol over stdio (the default)
  check        lint shell scripts, with ShellCheck's flags and output formats
  fmt          format shell scripts, with shfmt's flags
  version      print the version
  help         print this help, or the help of a command: shell-lsp help check

Run with no command, or with --stdio, to serve LSP to an editor.
`

func Run(env Env) int {
	if len(env.Args) == 0 {
		return serve(env)
	}
	command, args := env.Args[0], env.Args[1:]
	switch command {
	case "serve", "--stdio", "-stdio":
		if len(args) > 0 {
			fmt.Fprintf(env.Stderr, "%s: %s takes no arguments, got %q\n", programName, command, args)
			return statusUsage
		}
		return serve(env)
	case "check":
		return runCheck(env, args)
	case "fmt":
		return runFmt(env, args)
	case "version", "--version", "-version", "-V":
		fmt.Fprintln(env.Stdout, versionPrefix+env.Version)
		return statusOK
	case "help", "-h", "--help", "-help":
		return help(env, args)
	}
	fmt.Fprintf(env.Stderr, "%s: unknown command %q\n\n%s", programName, command, usage)
	return statusUsage
}

func serve(env Env) int {
	if err := env.Serve(); err != nil {
		fmt.Fprintf(env.Stderr, "%s: %v\n", programName, err)
		return statusFailed
	}
	return statusOK
}

func help(env Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(env.Stdout, usage)
		return statusOK
	}
	switch args[0] {
	case "check":
		fmt.Fprint(env.Stdout, checkUsage)
		return statusOK
	case "fmt":
		fmt.Fprint(env.Stdout, fmtUsage)
		return statusOK
	}
	fmt.Fprintf(env.Stderr, "%s: no help for unknown command %q\n\n%s", programName, args[0], usage)
	return statusUsage
}
