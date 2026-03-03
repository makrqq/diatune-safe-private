package version

import "strings"

var semver = "0.0.4"

func Semver() string {
	v := strings.TrimSpace(semver)
	if v == "" {
		return "dev"
	}
	return v
}
