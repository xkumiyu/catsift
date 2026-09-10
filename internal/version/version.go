// Package version resolves the version embedded in a CatSift binary.
package version

import (
	"runtime/debug"
	"strings"
)

const Development = "dev"

// Version can be overridden by release builds with -ldflags -X. Tagged module
// installs use the version recorded in Go build information when no override
// is provided.
var Version = Development

// String returns the most specific version available for the running binary.
func String() string {
	var info *debug.BuildInfo
	if buildInfo, ok := debug.ReadBuildInfo(); ok {
		info = buildInfo
	}
	return resolve(Version, info)
}

func resolve(linked string, info *debug.BuildInfo) string {
	linked = strings.TrimSpace(linked)
	if linked != "" && linked != Development {
		return linked
	}
	if info != nil {
		moduleVersion := strings.TrimSpace(info.Main.Version)
		if moduleVersion != "" && moduleVersion != "(devel)" {
			return moduleVersion
		}
	}
	if linked != "" {
		return linked
	}
	return Development
}
