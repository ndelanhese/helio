package helio_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestSmokeScriptHasBoundedSecretSafeCleanupContract(t *testing.T) {
	script := readFile(t, "scripts/smoke.sh")
	for _, required := range []string{
		"set -euo pipefail",
		"umask 077",
		"trap cleanup EXIT INT TERM HUP",
		"helio-smoke-",
		"docker image inspect",
		"{{.Id}}",
		"SECONDS + 60",
		"/health/ready",
		"/health/components",
		"/api/v1/auth/session",
		"/api/v1/settings",
		"/api/v1/history",
		"/api/v1/data/backup",
		"docker volume rm",
		"HELIO_SMOKE_FAIL_AFTER_VOLUME",
		"--config \"$csrf_config\"",
		"chmod 0600 \"$csrf_config\"",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("smoke script does not contain %q", required)
		}
	}
	for _, forbidden := range []string{"set -x", "echo $password", "echo ${password", "docker run --name helio-smoke", `--header "X-CSRF-Token: $csrf"`} {
		if strings.Contains(script, forbidden) {
			t.Errorf("smoke script contains unsafe construct %q", forbidden)
		}
	}
}

func TestSmokeFixtureIsBuildTaggedAndExcludedFromProductionBuildInputs(t *testing.T) {
	fixture := readFile(t, "scripts/smokefixture/main.go")
	if !strings.HasPrefix(fixture, "//go:build smoke\n") {
		t.Fatal("smoke fixture is not protected by the smoke build tag")
	}
	dockerfile := readFile(t, "Dockerfile")
	if strings.Contains(dockerfile, "COPY scripts/") {
		t.Fatal("production Dockerfile copies the smoke fixture")
	}
}

func TestBackupRestoreRunbookStatesOfflineSafetyBoundaries(t *testing.T) {
	doc := strings.ToLower(readFile(t, "docs/backup-restore.md"))
	for _, required := range []string{
		"stop", "offline", "copy", "integrity_check", "ownership", "restore", "rollback",
		"encrypt", "version compatibility", "never overwrite", "live volume",
	} {
		if !strings.Contains(doc, required) {
			t.Errorf("backup runbook does not contain %q", required)
		}
	}
	if !strings.Contains(doc, "docker compose ps --all --quiet helio") {
		t.Fatal("offline copy does not locate the stopped Compose container")
	}
	if strings.Contains(doc, "docker compose ps -q helio") {
		t.Fatal("offline copy uses a running-containers-only Compose lookup")
	}
	if !strings.Contains(doc, "free space") || !strings.Contains(doc, "database size") {
		t.Fatal("online backup does not document same-volume free-space requirement")
	}
	command := exec.Command("docker", "compose", "ps", "--help")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("docker compose ps --help: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "--all") || !strings.Contains(string(output), "--quiet") {
		t.Fatal("documented stopped-container flags are unsupported by installed Docker Compose")
	}
}

func TestAPIStoreDoesNotRequireUnusedStreamingBackupMethod(t *testing.T) {
	api := readFile(t, "internal/api/api.go")
	if strings.Contains(api, "Backup(context.Context, io.Writer) error") {
		t.Fatal("API Store retains unused Backup writer method")
	}
}
