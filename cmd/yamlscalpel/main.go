// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"

	"github.com/sapcc/go-bits/logg"
	"github.com/sapcc/go-bits/must"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		fmt.Fprintln(os.Stderr, buildUsageText())
		os.Exit(1)

	case slices.Contains(args, "--help"):
		fmt.Fprintln(os.Stdout, buildUsageText())

	case slices.Contains(args, "--version"):
		version := "<unknown>"
		if bi, ok := debug.ReadBuildInfo(); ok {
			version = bi.Main.Version
		}
		fmt.Printf("go-bits/cmd/yamlscalpel version %s\n", version)

	default:
		run(args)
	}
}

func run(args []string) {
	values := make(map[string]any, len(args))
	for _, arg := range args {
		path, value, err := parseArgument(arg)
		must.Succeed(err)
		if values[path] != nil {
			logg.Fatal("multiple values given for path %q", path)
		}
		values[path] = value
	}

	buf := must.Return(io.ReadAll(os.Stdin))
	buf = must.Return(update(buf, values))
	_ = must.Return(os.Stdout.Write(buf))
}

func buildUsageText() string {
	str := fmt.Sprintf(usageText, os.Args[0])
	str = strings.TrimSpace(str)
	return strings.ReplaceAll(str, "\t", "  ")
}

const usageText = `
Usage:
	%[1]s --help       - Show this help message.
	%[1]s --version    - Report the version of the tool.
	%[1]s <change>...  - See below.

This tool parses a YAML document presented on stdin, and changes specific values as requested in the command line,
producing the altered YAML document on stdout in a way that preserves comments and document structure as much as possible.
Each positional argument must specify one field to be changed, in the form "<path>=<type>:<value>":

	- The path identifies the field that is being changed. Path elements are separated by dots.
		For example, "foo.2.bar" refers to the key "bar" within the array element with index 2 within the key "foo".
	- The type can be one of "string", "int", "float" or "bool". This decides how the value is parsed.

For example, when invoked as '%[1]s image.tag=string:v1.2.3 worker.replicas=int:4' with the following stdin:

		image:
			repository: foo/bar
			tag: v1.2.0

		api:
			replicas: 2

		worker:
			replicas: 1

The following output will be produced:

		image:
			repository: foo/bar
			tag: v1.2.3
		api:
			replicas: 2
		worker:
			replicas: 4

Like in this example, the output may not exactly match the input in terms of whitespace (though comments should always be preserved).
When modifying a file in place, we recommend rejecting whitespace diffs, e.g. like this when using Git:

		%[1]s $CHANGES < my-file.yaml > my-file.yaml.new
		mv my-file.yaml.new my-file.yaml
		git diff -U0 -w --no-color my-file.yaml | git apply --cached --ignore-whitespace --unidiff-zero -
`

// ^ The `git diff/apply` line is courtesy of <https://stackoverflow.com/a/45486981>.

func parseArgument(arg string) (path string, value any, err error) {
	path, typeAndValue, ok := strings.Cut(arg, "=")
	if !ok {
		return "", nil, fmt.Errorf(`expected positional argument formatted as "path=type:value", but got %q`, arg)
	}
	typeStr, valueStr, ok := strings.Cut(typeAndValue, ":")
	if !ok {
		return "", nil, fmt.Errorf(`expected positional argument formatted as "path=type:value", but got %q`, arg)
	}
	switch typeStr {
	case "string":
		return path, valueStr, nil
	case "int":
		value, err := strconv.ParseInt(valueStr, 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("invalid value in positional argument %q: %w", arg, err)
		}
		return path, value, nil
	case "float":
		value, err := strconv.ParseFloat(valueStr, 64)
		if err != nil {
			return "", nil, fmt.Errorf("invalid value in positional argument %q: %w", arg, err)
		}
		return path, value, nil
	case "bool":
		value, err := strconv.ParseBool(valueStr)
		if err != nil {
			return "", nil, fmt.Errorf("invalid value in positional argument %q: %w", arg, err)
		}
		return path, value, nil
	default:
		return "", nil, fmt.Errorf(`invalid type in positional argument %q: expected one of "string", "int", "float" or "bool", but got %q`, arg, typeStr)
	}
}
