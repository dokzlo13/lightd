// Package buildinfo exposes immutable build identity without requiring configuration.
package buildinfo

import "runtime"

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

func Info() map[string]string {
	return map[string]string{"version": Version, "commit": Commit, "built_at": BuildTime, "go": runtime.Version()}
}
