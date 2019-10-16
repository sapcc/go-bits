// +build !windows

package env

import (
	"os"
)

func Get(s string) string {
	return os.Getenv(s)
}
