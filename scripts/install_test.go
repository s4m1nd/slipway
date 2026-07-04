package scripts

import (
	"os"
	"strings"
	"testing"
)

func TestInstallScriptSupportsMacAndLinuxChecksumTools(t *testing.T) {
	data, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	script := string(data)
	for _, want := range []string{"shasum -a 256", "sha256sum -c"} {
		if !strings.Contains(script, want) {
			t.Fatalf("install.sh missing checksum verifier %q", want)
		}
	}
}

func TestReleaseBuildScriptSupportsMacAndLinuxChecksumTools(t *testing.T) {
	data, err := os.ReadFile("release/build.sh")
	if err != nil {
		t.Fatalf("read release/build.sh: %v", err)
	}
	script := string(data)
	for _, want := range []string{"shasum -a 256", "sha256sum"} {
		if !strings.Contains(script, want) {
			t.Fatalf("release/build.sh missing checksum support %q", want)
		}
	}
}

func TestMakeCheckAndCIExerciseAlphaChecklist(t *testing.T) {
	makefileData, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	ciData, err := os.ReadFile("../.github/workflows/ci.yml")
	if err != nil {
		t.Fatalf("read ci workflow: %v", err)
	}
	makefile := string(makefileData)
	ci := string(ciData)

	for _, want := range []string{
		"fmt-check:",
		"check: fmt-check",
		"go test ./...",
		`@files="$$(gofmt -l cmd internal)"; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }`,
		`@for script in scripts/live/*.sh scripts/install.sh scripts/release/build.sh; do sh -n "$$script"; done`,
		"go run ./cmd/slipway version",
		"go run ./cmd/slipway validate -c slipway.live.example.yml --env production",
		"go run ./cmd/slipway sync-proxy -c slipway.example.yml --env production --dry-run",
		"go run ./cmd/slipway cleanup -c slipway.example.yml --env production --dry-run",
		"go run ./cmd/slipway logs -c slipway.example.yml --env production --service web --dry-run",
		"scripts/install.sh --dry-run",
	} {
		if !strings.Contains(makefile, want) {
			t.Fatalf("Makefile check target missing %q:\n%s", want, makefile)
		}
	}
	if !strings.Contains(ci, "make check") {
		t.Fatalf("CI should run make check:\n%s", ci)
	}
}

func TestReleaseWorkflowPublishesTaggedArtifacts(t *testing.T) {
	data, err := os.ReadFile("../.github/workflows/release.yml")
	if err != nil {
		t.Fatalf("read release workflow: %v", err)
	}
	workflow := string(data)
	for _, want := range []string{
		`- "v*"`,
		"contents: write",
		"make check",
		`scripts/release/build.sh "${GITHUB_REF_NAME}"`,
		"gh release create",
		"dist/slipway_darwin_amd64",
		"dist/slipway_darwin_arm64",
		"dist/slipway_linux_amd64",
		"dist/slipway_linux_arm64",
		"dist/checksums.txt",
		"--prerelease",
	} {
		if !strings.Contains(workflow, want) {
			t.Fatalf("release workflow missing %q:\n%s", want, workflow)
		}
	}
}

func TestReadmeDoesNotLinkIgnoredLiveConfig(t *testing.T) {
	readmeData, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	gitignoreData, err := os.ReadFile("../.gitignore")
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	readme := string(readmeData)
	gitignore := string(gitignoreData)

	if !strings.Contains(gitignore, "slipway.live.yml") {
		t.Fatal("test expects slipway.live.yml to stay ignored")
	}
	if strings.Contains(readme, "(./slipway.live.yml)") {
		t.Fatalf("README links to ignored slipway.live.yml:\n%s", readme)
	}
	if !strings.Contains(readme, "copy [`slipway.live.example.yml`](./slipway.live.example.yml) to ignored `slipway.live.yml`") {
		t.Fatalf("README should explain that slipway.live.yml is local-only:\n%s", readme)
	}
}

func TestInstallerAndReadmeUsePublishedRepoPath(t *testing.T) {
	installData, err := os.ReadFile("install.sh")
	if err != nil {
		t.Fatalf("read install.sh: %v", err)
	}
	readmeData, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	installScript := string(installData)
	readme := string(readmeData)

	if !strings.Contains(installScript, "REPO=${SLIPWAY_REPO:-s4m1nd/slipway}") {
		t.Fatalf("install.sh should default to the published repo path:\n%s", installScript)
	}
	if !strings.Contains(installScript, "Defaults to s4m1nd/slipway.") {
		t.Fatalf("install.sh help should document the published repo path:\n%s", installScript)
	}
	if !strings.Contains(readme, "https://raw.githubusercontent.com/s4m1nd/slipway/main/scripts/install.sh") {
		t.Fatalf("README install command should point at the published repo path:\n%s", readme)
	}
}

func TestReadmeLeadsWithPinnedAlphaInstall(t *testing.T) {
	readmeData, err := os.ReadFile("../README.md")
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	readme := string(readmeData)
	pinned := "SLIPWAY_VERSION=v0.1.0-alpha.1 bash"
	latest := "scripts/install.sh | bash"

	pinnedIndex := strings.Index(readme, pinned)
	if pinnedIndex < 0 {
		t.Fatalf("README should show the pinned alpha install command:\n%s", readme)
	}
	latestIndex := strings.Index(readme, latest)
	if latestIndex < 0 {
		t.Fatalf("README should still document the stable latest install command:\n%s", readme)
	}
	if latestIndex < pinnedIndex {
		t.Fatalf("README should lead with pinned alpha install before latest:\n%s", readme)
	}
	if !strings.Contains(readme, "After the first stable release") {
		t.Fatalf("README should explain that unpinned latest is for stable releases:\n%s", readme)
	}
}
