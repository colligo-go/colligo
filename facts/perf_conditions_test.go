package facts

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

const (
	conditionTurboOff = "intel_pstate_no_turbo"
	conditionGovernor = "cpu_governor"
	conditionOnMains  = "on_mains_power"
)

func readTrimmed(path string) (string, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(raw)), true
}

func readPerfConditions(sysfs string) map[string]string {
	conditions := map[string]string{}
	if v, ok := readTrimmed(filepath.Join(sysfs, "devices/system/cpu/intel_pstate/no_turbo")); ok {
		conditions[conditionTurboOff] = v
	}
	governors := map[string]bool{}
	files, _ := filepath.Glob(filepath.Join(sysfs, "devices/system/cpu/cpu[0-9]*/cpufreq/scaling_governor"))
	for _, file := range files {
		if v, ok := readTrimmed(file); ok {
			governors[v] = true
		}
	}
	if len(governors) > 0 {
		names := make([]string, 0, len(governors))
		for name := range governors {
			names = append(names, name)
		}
		sort.Strings(names)
		conditions[conditionGovernor] = strings.Join(names, ",")
	}
	mains, _ := filepath.Glob(filepath.Join(sysfs, "class/power_supply/*"))
	exposed, plugged := false, false
	for _, supply := range mains {
		if kind, ok := readTrimmed(filepath.Join(supply, "type")); !ok || kind != "Mains" {
			continue
		}
		if online, ok := readTrimmed(filepath.Join(supply, "online")); ok {
			exposed = true
			plugged = plugged || online == "1"
		}
	}
	if exposed {
		conditions[conditionOnMains] = fmt.Sprint(plugged)
	}
	return conditions
}

func perfConditionFix(key, want string) string {
	switch key {
	case conditionTurboOff:
		return fmt.Sprintf("echo %s | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo", want)
	case conditionGovernor:
		return fmt.Sprintf("echo %s | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor", want)
	case conditionOnMains:
		if want == "true" {
			return "connect the charger"
		}
		return "disconnect the charger"
	}
	return "restore it"
}

func perfConditionMismatches(recorded, now map[string]string) []string {
	keys := make([]string, 0, len(recorded))
	for key := range recorded {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var mismatches []string
	for _, key := range keys {
		want := recorded[key]
		got, exposed := now[key]
		switch {
		case !exposed:
			mismatches = append(mismatches, fmt.Sprintf("%s was %q when the baseline was recorded and this machine no longer exposes it: the baseline cannot be compared", key, want))
		case got != want:
			mismatches = append(mismatches, fmt.Sprintf("%s is %q now and was %q when the baseline was recorded; fix: %s", key, got, want, perfConditionFix(key, want)))
		}
	}
	return mismatches
}

func writeSysfs(t *testing.T, root, name, value string) {
	t.Helper()
	path := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPerfConditionsAreReadFromSysfs(t *testing.T) {
	root := t.TempDir()
	writeSysfs(t, root, "devices/system/cpu/intel_pstate/no_turbo", "1")
	writeSysfs(t, root, "devices/system/cpu/cpu0/cpufreq/scaling_governor", "performance")
	writeSysfs(t, root, "devices/system/cpu/cpu1/cpufreq/scaling_governor", "performance")
	writeSysfs(t, root, "class/power_supply/ADP0/type", "Mains")
	writeSysfs(t, root, "class/power_supply/ADP0/online", "1")
	writeSysfs(t, root, "class/power_supply/BAT0/type", "Battery")
	got := readPerfConditions(root)
	want := map[string]string{conditionTurboOff: "1", conditionGovernor: "performance", conditionOnMains: "true"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("conditions %v, want %v", got, want)
	}
}

func TestPerfConditionsThatTheMachineDoesNotExposeAreLeftOut(t *testing.T) {
	root := t.TempDir()
	writeSysfs(t, root, "devices/system/cpu/cpu0/cpufreq/scaling_governor", "schedutil")
	writeSysfs(t, root, "class/power_supply/BAT0/type", "Battery")
	got := readPerfConditions(root)
	if len(got) != 1 || got[conditionGovernor] != "schedutil" {
		t.Fatalf("conditions %v, want only the governor", got)
	}
}

func TestPerfConditionMismatchNamesTheConditionAndTheFix(t *testing.T) {
	recorded := map[string]string{conditionTurboOff: "1", conditionGovernor: "performance", conditionOnMains: "true"}
	now := map[string]string{conditionTurboOff: "0", conditionGovernor: "performance", conditionOnMains: "false"}
	mismatches := perfConditionMismatches(recorded, now)
	if len(mismatches) != 2 {
		t.Fatalf("mismatches %v, want 2", mismatches)
	}
	if !strings.Contains(mismatches[0], conditionTurboOff) || !strings.Contains(mismatches[0], "echo 1 | sudo tee /sys/devices/system/cpu/intel_pstate/no_turbo") {
		t.Fatalf("turbo mismatch does not say what to do: %s", mismatches[0])
	}
	if !strings.Contains(mismatches[1], conditionOnMains) || !strings.Contains(mismatches[1], "connect the charger") {
		t.Fatalf("mains mismatch does not say what to do: %s", mismatches[1])
	}
}

func TestPerfConditionNotRecordedIsNotRequired(t *testing.T) {
	recorded := map[string]string{conditionGovernor: "performance"}
	now := map[string]string{conditionGovernor: "performance", conditionTurboOff: "0", conditionOnMains: "false"}
	if mismatches := perfConditionMismatches(recorded, now); len(mismatches) != 0 {
		t.Fatalf("unexpected mismatches %v", mismatches)
	}
	if mismatches := perfConditionMismatches(nil, now); len(mismatches) != 0 {
		t.Fatalf("a baseline with no conditions must not require any: %v", mismatches)
	}
}

func TestPerfConditionNoLongerExposedBlocksTheComparison(t *testing.T) {
	mismatches := perfConditionMismatches(map[string]string{conditionTurboOff: "1"}, map[string]string{})
	if len(mismatches) != 1 || !strings.Contains(mismatches[0], "no longer exposes") {
		t.Fatalf("mismatches %v", mismatches)
	}
}
