package slipway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	canonicalModulePath = "github.com/s4m1nd/slipway"
	legacyModulePath    = "github.com/" + "slipway" + "/slipway"
)

func TestCanonicalModulePathIsUsedEverywhere(t *testing.T) {
	goMod, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !strings.Contains(string(goMod), "module "+canonicalModulePath+"\n") {
		t.Fatalf("go.mod should declare module %s:\n%s", canonicalModulePath, string(goMod))
	}

	buildScript, err := os.ReadFile(filepath.Join("scripts", "release", "build.sh"))
	if err != nil {
		t.Fatalf("read release build script: %v", err)
	}
	for _, field := range []string{"Version", "Commit", "Date"} {
		want := "-X " + canonicalModulePath + "/internal/cli." + field
		if !strings.Contains(string(buildScript), want) {
			t.Fatalf("release build script should set %s with canonical module path:\n%s", field, string(buildScript))
		}
	}

	var legacyReferences []string
	err = filepath.WalkDir(".", func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			switch filepath.Base(path) {
			case ".git", ".terraform", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if !shouldCheckModulePath(path) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(data), legacyModulePath) {
			legacyReferences = append(legacyReferences, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan repo for legacy module path: %v", err)
	}
	if len(legacyReferences) > 0 {
		t.Fatalf("legacy module path %s found in: %s", legacyModulePath, strings.Join(legacyReferences, ", "))
	}
}

func shouldCheckModulePath(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".sh", ".md", ".yml", ".yaml", ".tf", ".tftpl", ".mod", ".sum":
		return true
	case ".example":
		return strings.HasSuffix(path, ".tfvars.example")
	default:
		return false
	}
}
