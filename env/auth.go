package env

import (
	"github.com/gophercloud/gophercloud"
)

var nilOptions = gophercloud.AuthOptions{}

/*
AuthOptionsFromEnv fills out an identity.AuthOptions structure with the
settings found on the various OpenStack OS_* environment variables.

The following variables provide sources of truth: OS_AUTH_URL, OS_USERNAME,
OS_PASSWORD and OS_PROJECT_ID.

Of these, OS_USERNAME, OS_PASSWORD, and OS_AUTH_URL must have settings,
or an error will result.  OS_PROJECT_ID, is optional.

OS_TENANT_ID and OS_TENANT_NAME are deprecated forms of OS_PROJECT_ID and
OS_PROJECT_NAME and the latter are expected against a v3 auth api.

If OS_PROJECT_ID and OS_PROJECT_NAME are set, they will still be referred
as "tenant" in Gophercloud.

If OS_PROJECT_NAME is set, it requires OS_PROJECT_ID to be set as well to
handle projects not on the default domain.

To use this function, first set the OS_* environment variables (for example,
by sourcing an `openrc` file), then:

	opts, err := AuthOptionsFromEnv()
	provider, err := openstack.AuthenticatedClient(opts)
*/
func AuthOptionsFromEnv() (gophercloud.AuthOptions, error) {
	authURL := Get("OS_AUTH_URL")
	username := Get("OS_USERNAME")
	userID := Get("OS_USERID")
	password := Get("OS_PASSWORD")
	tenantID := Get("OS_TENANT_ID")
	tenantName := Get("OS_TENANT_NAME")
	domainID := Get("OS_DOMAIN_ID")
	domainName := Get("OS_DOMAIN_NAME")
	applicationCredentialID := Get("OS_APPLICATION_CREDENTIAL_ID")
	applicationCredentialName := Get("OS_APPLICATION_CREDENTIAL_NAME")
	applicationCredentialSecret := Get("OS_APPLICATION_CREDENTIAL_SECRET")

	token := Get("OS_AUTH_TOKEN")
	if token == "" {
		// fallback to an old env name
		token = Get("OS_TOKEN")
	}

	// If OS_PROJECT_ID is set, overwrite tenantID with the value.
	if v := Get("OS_PROJECT_ID"); v != "" {
		tenantID = v
	}

	// If OS_PROJECT_NAME is set, overwrite tenantName with the value.
	if v := Get("OS_PROJECT_NAME"); v != "" {
		tenantName = v
	}

	// If OS_PROJECT_DOMAIN_NAME is set, overwrite domainName with the value.
	if v := Get("OS_PROJECT_DOMAIN_NAME"); v != "" {
		domainName = v
	}

	// If OS_PROJECT_DOMAIN_ID is set, overwrite domainID with the value.
	if v := Get("OS_PROJECT_DOMAIN_ID"); v != "" {
		domainID = v
	}

	if authURL == "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_AUTH_URL",
		}
		return nilOptions, err
	}

	if userID == "" && username == "" && token == "" {
		// Empty username and userID could be ignored, when applicationCredentialID and applicationCredentialSecret are set
		if applicationCredentialID == "" && applicationCredentialSecret == "" {
			err := gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_USERID", "OS_USERNAME", "OS_AUTH_TOKEN"},
			}
			return nilOptions, err
		}
	}

	if password == "" && applicationCredentialID == "" && applicationCredentialName == "" && token == "" {
		err := gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
			EnvironmentVariables: []string{"OS_PASSWORD", "OS_AUTH_TOKEN"},
		}
		return nilOptions, err
	}

	if (applicationCredentialID != "" || applicationCredentialName != "") && applicationCredentialSecret == "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_APPLICATION_CREDENTIAL_SECRET",
		}
		return nilOptions, err
	}

	if domainID == "" && domainName == "" && tenantID == "" && tenantName != "" {
		err := gophercloud.ErrMissingEnvironmentVariable{
			EnvironmentVariable: "OS_PROJECT_ID",
		}
		return nilOptions, err
	}

	if applicationCredentialID == "" && applicationCredentialName != "" && applicationCredentialSecret != "" {
		if userID == "" && username == "" && token == "" {
			return nilOptions, gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_USERID", "OS_USERNAME", "OS_AUTH_TOKEN"},
			}
		}
		if username != "" && domainID == "" && domainName == "" {
			return nilOptions, gophercloud.ErrMissingAnyoneOfEnvironmentVariables{
				EnvironmentVariables: []string{"OS_DOMAIN_ID", "OS_DOMAIN_NAME"},
			}
		}
	}

	var scope *gophercloud.AuthScope
	if token != "" {
		// scope is required for the token auth
		username = ""
		userID = ""
		password = ""

		scope = &gophercloud.AuthScope{
			ProjectID:   tenantID,
			ProjectName: tenantName,
			DomainID:    domainID,
			DomainName:  domainName,
		}

		domainName = ""
		domainID = ""

		tenantName = ""
		tenantID = ""
	}

	ao := gophercloud.AuthOptions{
		IdentityEndpoint:            authURL,
		UserID:                      userID,
		Username:                    username,
		Password:                    password,
		TenantID:                    tenantID,
		TenantName:                  tenantName,
		DomainID:                    domainID,
		DomainName:                  domainName,
		ApplicationCredentialID:     applicationCredentialID,
		ApplicationCredentialName:   applicationCredentialName,
		ApplicationCredentialSecret: applicationCredentialSecret,
		TokenID:                     token,
		Scope:                       scope,
	}

	return ao, nil
}
