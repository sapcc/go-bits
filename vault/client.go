// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package vault

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/vault/api"
	"github.com/hashicorp/vault/api/cliconfig"
)

// CreateClient creates and returns a vault api client and supports authentication using VAULT_TOKEN, VAULT_ROLE_ID and VAULT_SECRET_ID or ~/.vault-token
func CreateClient() (*api.Client, error) {
	cfg := api.DefaultConfig()
	if cfg.Error != nil {
		return nil, fmt.Errorf("while reading Vault config from environment: %w", cfg.Error)
	}

	client, err := api.NewClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("while initializing Vault client: %w", err)
	}

	token := client.Token()

	if token == "" {
		helper, err := cliconfig.DefaultTokenHelper()
		if err != nil {
			return nil, fmt.Errorf("failed to get token helper: %w", err)
		}
		token, err = helper.Get()
		if err != nil {
			return nil, fmt.Errorf("failed to get token from token helper: %w", err)
		}

		client.SetToken(token)
	}

	if token == "" {
		if os.Getenv("VAULT_ROLE_ID") != "" && os.Getenv("VAULT_SECRET_ID") != "" {
			// perform app-role authentication if necessary
			resp, err := client.Logical().Write("auth/approle/login", map[string]any{
				"role_id":   os.Getenv("VAULT_ROLE_ID"),
				"secret_id": os.Getenv("VAULT_SECRET_ID"),
			})
			if err != nil {
				return nil, fmt.Errorf("while obtaining approle token: %w", err)
			}
			client.SetToken(resp.Auth.ClientToken)
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("while fetching home directory: %w", err)
			}
			vaultTokenFile := homeDir + "/.vault-token"
			bytes, err := os.ReadFile(vaultTokenFile)
			if errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("Some environment variables are missing! For pipelines makes sure VAULT_ROLE_ID and VAULT_SECRET_ID are set and for manual invocations make sure VAULT_TOKEN is set or you previously logged into vault cli. DO NOT use the variables the other way around!") //nolint:staticcheck // we want it like this
			} else if err != nil {
				return nil, fmt.Errorf("failed reading %s: %w", vaultTokenFile, err)
			}
			client.SetToken(strings.TrimSpace(string(bytes)))
		}
	}

	return client, nil
}
