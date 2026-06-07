// Command soundbuddy inspects the local audio stack and explains, in plain
// English, how it is set up. It is strictly read-only.
package main

import (
	"flag"
	"fmt"
	"os"

	"soundbuddy/internal/collect"
	"soundbuddy/internal/explain"
)

func main() {
	var (
		noColor  = flag.Bool("no-color", false, "disable ANSI colour output")
		glossary = flag.Bool("glossary", false, "include the glossary section defining all terms")
	)
	flag.BoolVar(glossary, "g", false, "shorthand for --glossary")
	flag.Usage = usage
	flag.Parse()

	facts := collect.All()

	report := explain.Render(facts, explain.Options{
		Color:    useColor(*noColor),
		Glossary: *glossary,
	})
	fmt.Print(report)
}

func usage() {
	fmt.Fprintf(os.Stderr, `soundbuddy — full readout of how audio is set up on this machine (read-only)

Usage:
  soundbuddy [flags]

Flags:
      --no-color    disable ANSI colour output
  -g, --glossary    include the glossary section defining all terms
`)
}

// useColor enables colour only when not disabled and stdout is a terminal, so
// piping to a file or pager stays clean.
func useColor(disabled bool) bool {
	if disabled || os.Getenv("NO_COLOR") != "" {
		return false
	}
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
