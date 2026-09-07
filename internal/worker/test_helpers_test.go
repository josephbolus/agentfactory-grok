package worker

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func writeFakeCodex(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nset -eu\nif [ \"${1:-}\" = \"--version\" ]; then echo 'codex-test'; exit 0; fi\nexit 0\n"), 0o700); err != nil {
		t.Fatal(err)
	}
}

func testOptions(codexPath string) Options {
	return Options{
		GitExecutable: "git", GitHubExecutable: filepath.Join(filepath.Dir(codexPath), "unavailable-gh"),
		RuntimeExecutable: codexPath, WorkerVersion: "test", PollInterval: 20 * time.Millisecond,
		HealthInterval: 300 * time.Millisecond, RegistrationInterval: 25 * time.Millisecond,
		LeaseRenewInterval: 100 * time.Millisecond, LeaseRetryInterval: 50 * time.Millisecond,
		TransportBackoffMin: 20 * time.Millisecond, TransportBackoffMax: 100 * time.Millisecond,
		ShutdownTimeout: 15 * time.Second,
	}
}

func runTestCommand(t *testing.T, directory, name string, arguments ...string) string {
	t.Helper()
	command := exec.Command(name, arguments...)
	command.Dir = directory
	body, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v: %s", name, arguments, err, body)
	}
	return string(body)
}
