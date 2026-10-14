package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	message := flag.String("message", "", "fully-qualified root message type (auto-detected by default)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [-message package.Message] PAYLOAD SCHEMA\n\n", os.Args[0])
		fmt.Fprintln(flag.CommandLine.Output(), "SCHEMA may be one .proto file or a directory containing .proto files.")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}

	program := tea.NewProgram(newStartupModel(flag.Arg(0), flag.Arg(1), *message), tea.WithAltScreen())
	final, err := program.Run()
	if err != nil {
		fatal("run terminal UI", err)
	}
	if startup, ok := final.(startupModel); ok && startup.err != nil {
		fatal("load input", startup.err)
	}
}

func fatal(action string, err error) {
	fmt.Fprintf(os.Stderr, "protoview: %s: %v\n", action, err)
	os.Exit(1)
}
