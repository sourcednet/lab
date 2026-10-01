// Command sourced-lab is the sourced.net lab: the benchmark harness (fetch a corpus,
// run experiments, compare results) and the demo, which runs sample
// publishers, a resolver, and the MCP tools in one process.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
)

const version = "0.1.0-dev"

const usage = `sourced-lab — sourced.net benchmark and demo (spec sourced/1)

Usage:
  sourced-lab <command> [flags] [arguments]

Commands:
  bench     fetch a corpus, run experiments, compare results
  demo      run sample publishers, a resolver, and the MCP tools in one process
  version   print the version

Run "sourced-lab <command> -h" for a command's flags.
`

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	cmds := map[string]func(context.Context, []string, io.Writer, io.Writer) int{
		"bench": cmdBench,
		"demo":  cmdDemo,
	}
	switch name := args[0]; name {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		cmd, ok := cmds[name]
		if !ok {
			fmt.Fprintf(stderr, "sourced-lab: unknown command %q\n\n%s", name, usage)
			return 2
		}
		return cmd(ctx, args[1:], stdout, stderr)
	}
}
