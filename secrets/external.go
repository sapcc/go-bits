// SPDX-FileCopyrightText: 2023 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package secrets

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GetPasswordFromCommandIfRequested evaluates the $OS_PW_CMD environment
// variable if it exists and $OS_PASSWORD has not been provided.
func GetPasswordFromCommandIfRequested() error {
	pwCmd := os.Getenv("OS_PW_CMD")
	if pwCmd == "" || os.Getenv("OS_PASSWORD") != "" {
		return nil
	}
	// Retrieve user's password from external command.
	cmd := exec.Command("sh", "-c", pwCmd)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("could not execute OS_PW_CMD: %w", err)
	}
	os.Setenv("OS_PASSWORD", strings.TrimSuffix(string(out), "\n"))
	return nil
}
