package version

import "strings"

var semver = "0.0.3"

func Semver() string {
	v := strings.TrimSpace(semver)
	if v == "" {
		return "dev"
	}
	return v
}
