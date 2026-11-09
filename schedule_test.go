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

func TestQmdRefresh(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	fake := filepath.Join(dir, "qmd")
	// Fails on "embed" when FAIL_EMBED is set; records every call.
	os.WriteFile(fake, []byte("#!/bin/sh\necho \"$@\" >> \""+calls+"\"\n[ \"$1\" = embed ] && [ -n \"$FAIL_EMBED\" ] && exit 1\nexit 0\n"), 0o755)
	run := func(env ...string) (string, error) {
		cmd := exec.Command("bash", "scripts/macos/qmd-refresh.sh")
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	if out, err := run("QMD_BIN=" + fake); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if got, _ := os.ReadFile(calls); string(got) != "update\nembed\n" {
		t.Errorf("calls = %q, want update then embed", got)
	}

	os.Remove(calls)
	if _, err := run("QMD_BIN="+fake, "FAIL_EMBED=1"); err == nil {
		t.Error("a failing embed should exit non-zero")
	}

	if out, err := run("QMD_BIN=" + filepath.Join(dir, "missing")); err != nil || !strings.Contains(out, "skipping") {
		t.Errorf("missing qmd should be skipped: %v: %s", err, out)
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
