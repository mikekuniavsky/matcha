package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The macOS scheduling scripts are shell; --dry-run lets them be checked here without launchctl or pmset.
func TestInstallScheduleDryRun(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	run := func(args ...string) (string, error) {
		out, err := exec.Command("bash", append([]string{"scripts/macos/install-schedule.sh", "--dry-run"}, args...)...).CombinedOutput()
		return string(out), err
	}

	out, err := run("--time", "00:05")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{
		"<key>Hour</key><integer>0</integer><key>Minute</key><integer>5</integer>",
		"<key>StartInterval</key><integer>900</integer>",
		"run-daily.sh</string>",
		"sudo pmset repeat wakeorpoweron MTWRFSU 00:02:00",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}

	// The wake time wraps past midnight.
	if out, _ := run("--time", "00:01", "--wake-lead", "5"); !strings.Contains(out, "MTWRFSU 23:56:00") {
		t.Errorf("wrap: %s", out)
	}
	if out, _ := run("--time", "07:30", "--no-wake"); strings.Contains(out, "pmset") {
		t.Errorf("--no-wake still schedules a wake: %s", out)
	}
	for _, bad := range []string{"25:00", "7", ""} {
		if _, err := run("--time", bad); err == nil {
			t.Errorf("--time %q accepted", bad)
		}
	}
}

func TestRunDailySkipsSecondRun(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	stamp := filepath.Join(t.TempDir(), "last-run")
	os.WriteFile(stamp, []byte(time.Now().Format("2006-01-02")+"\n"), 0o644)
	cmd := exec.Command("bash", "scripts/macos/run-daily.sh")
	cmd.Env = append(os.Environ(), "MATCHA_STAMP="+stamp, "MATCHA_DIR="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil || !strings.Contains(string(out), "already ran today") {
		t.Errorf("%v: %s", err, out)
	}
}
