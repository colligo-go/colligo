package buildinfo

import "runtime/debug"

var (
	injectedVersion string
	injectedCommit  string
)

func Version() string {
	info, ok := debug.ReadBuildInfo()
	return versionOf(info, ok, injectedVersion)
}

func GitSHA() string {
	info, ok := debug.ReadBuildInfo()
	return commitOf(info, ok, injectedCommit)
}

func versionOf(info *debug.BuildInfo, ok bool, injected string) string {
	if ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	if injected != "" {
		return injected
	}
	return "dev"
}

func commitOf(info *debug.BuildInfo, ok bool, injected string) string {
	if ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	if injected != "" {
		return injected
	}
	return "unknown"
}
