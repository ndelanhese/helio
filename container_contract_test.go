package helio_test

import (
	"os"
	"strings"
	"testing"
)

func TestContainerSecretFilesAreIgnored(t *testing.T) {
	assertFileContainsLine(t, ".gitignore", "deploy/helio.env")
	assertFileContainsLine(t, ".dockerignore", "deploy/helio.env")
}

func TestDockerfileUsesPinnedNarrowDistrolessBuild(t *testing.T) {
	dockerfile := readFile(t, "Dockerfile")

	if strings.Contains(dockerfile, "COPY . .") {
		t.Fatal("Dockerfile copies the entire repository into the build context")
	}
	for _, required := range []string{
		"# syntax=docker/dockerfile:1.10@sha256:",
		"ARG SOURCE_DATE_EPOCH=0",
		"FROM node:24.17.0-bookworm-slim@sha256:",
		"FROM golang:1.26.5-bookworm@sha256:",
		"FROM gcr.io/distroless/static-debian12:nonroot@sha256:",
		"COPY cmd/ ./cmd/",
		"COPY internal/ ./internal/",
		"COPY --from=web",
		"COPY --chown=65532:65532 --from=data",
	} {
		if !strings.Contains(dockerfile, required) {
			t.Errorf("Dockerfile does not contain %q", required)
		}
	}
	if strings.Contains(dockerfile, "apt-get") {
		t.Fatal("Dockerfile runtime depends on mutable apt repositories")
	}
}

func TestComposeUsesSafeBindAndHardenedTmpfs(t *testing.T) {
	compose := readFile(t, "compose.yaml")
	if !strings.Contains(compose, `"${HELIO_BIND_IP:-127.0.0.1}:8080:8080"`) {
		t.Fatal("Compose does not default its published port to loopback")
	}
	if !strings.Contains(compose, "/tmp:rw,noexec,nosuid,nodev,size=16m,mode=1777") {
		t.Fatal("Compose tmpfs is missing required limits or mount hardening")
	}
}

func assertFileContainsLine(t *testing.T, path, want string) {
	t.Helper()
	for _, line := range strings.Split(readFile(t, path), "\n") {
		if line == want {
			return
		}
	}
	t.Fatalf("%s does not contain exact line %q", path, want)
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}
