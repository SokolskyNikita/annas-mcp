package version

import (
	_ "embed"
	"strings"
)

//go:embed version.txt
var embeddedVersion string

// Version is populated by release builds with the version from the Git tag.
// Development builds leave it empty and use the embedded version.txt value.
// Keeping this variable uninitialized makes it replaceable by go link -X.
var Version string

func GetVersion() string {
	if value := strings.TrimSpace(Version); value != "" {
		return value
	}
	return strings.TrimSpace(embeddedVersion)
}
