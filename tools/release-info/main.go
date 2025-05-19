// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
)

func handleErr(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

// tagHeadingRx matches headings with format: ## [X.Y.Z] - YEAR-MONTH-DAY
var tagHeadingRx = regexp.MustCompile(`^## \[?(?:v)?(\d+\.\d+\.\d+)\]? - \d{4}-\d{2}-\d{2}\s*$`)

// referenceLinkRx matches reference links at the end of changelog.
var referenceLinkRx = regexp.MustCompile(`^\[(unreleased|\d+\.\d+\.\d+)\]: http.*$`)

func main() {
	if len(os.Args) != 3 {
		handleErr(errors.New("usage: releaseinfo path-to-changelog-file vX.Y.Z"))
	}

	tag := strings.TrimPrefix(os.Args[2], "v")
	file, err := os.Open(os.Args[1])
	handleErr(err)
	defer file.Close()

	var releaseInfo []string
	in := false // true if we are inside the given tag's release block
	buf := bufio.NewScanner(file)
	for buf.Scan() {
		line := buf.Text()
		if ml := tagHeadingRx.FindStringSubmatch(line); len(ml) > 0 {
			if in {
				break
			}
			if ml[1] == tag {
				in = true
				continue
			}
		}

		if in && !referenceLinkRx.MatchString(line) {
			releaseInfo = append(releaseInfo, line)
		}
	}
	handleErr(buf.Err())

	if len(releaseInfo) == 0 {
		handleErr(fmt.Errorf("could not find release info for tag %q", os.Args[2]))
	}

	out := strings.TrimSpace(strings.Join(releaseInfo, "\n"))
	fmt.Println(out)
}
