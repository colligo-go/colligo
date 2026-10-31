package buildinfo

import (
	"runtime/debug"
	"testing"
)

func stamped(version, revision string) *debug.BuildInfo {
	info := &debug.BuildInfo{Main: debug.Module{Version: version}}
	if revision != "" {
		info.Settings = []debug.BuildSetting{{Key: "vcs.revision", Value: revision}}
	}
	return info
}

func TestTheVCSStampWinsOverAnInjectedVersion(t *testing.T) {
	info := stamped("v0.2.0", "b23beb1")
	if got := versionOf(info, true, "v9.9.9"); got != "v0.2.0" {
		t.Errorf("version %q", got)
	}
	if got := commitOf(info, true, "deadbeef"); got != "b23beb1" {
		t.Errorf("commit %q", got)
	}
}

func TestAnInjectedVersionFillsABuildWithoutStamp(t *testing.T) {
	for _, info := range []*debug.BuildInfo{stamped("(devel)", ""), stamped("", "")} {
		if got := versionOf(info, true, "v0.2.0"); got != "v0.2.0" {
			t.Errorf("version %q", got)
		}
		if got := commitOf(info, true, "b23beb1"); got != "b23beb1" {
			t.Errorf("commit %q", got)
		}
	}
	if got := versionOf(nil, false, "v0.2.0"); got != "v0.2.0" {
		t.Errorf("no build info: version %q", got)
	}
}

func TestWithoutStampOrInjectionTheBuildSaysSo(t *testing.T) {
	info := stamped("(devel)", "")
	if got := versionOf(info, true, ""); got != "dev" {
		t.Errorf("version %q", got)
	}
	if got := commitOf(info, true, ""); got != "unknown" {
		t.Errorf("commit %q", got)
	}
}
