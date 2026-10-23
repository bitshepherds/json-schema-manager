package app

import "runtime/debug"

// Version is the current version of jsm, set at build time. A build which doesn't set it, such as
// `go install`, reports the module version Go records in the binary instead.
var Version = "dev"

// buildVersion returns Version, or, if no build set it, the module version in the binary's build info.
func buildVersion() string {
	info, ok := debug.ReadBuildInfo()
	return versionFrom(Version, info, ok)
}

// versionFrom returns set, unless it is the default "dev" and info has the module's version.
// Go records "(devel)" when it builds a module from its own source tree, rather than a version of it.
func versionFrom(set string, info *debug.BuildInfo, ok bool) string {
	if set != "dev" || !ok || info.Main.Version == "" || info.Main.Version == "(devel)" {
		return set
	}
	return info.Main.Version
}
