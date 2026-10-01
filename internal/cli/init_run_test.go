package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// initConfigAt runs tagctl init --name path and returns its output and error.
func initConfigAt(t *testing.T, path string) (string, error) {
	t.Helper()

	flag := initCmd.Flags().Lookup("name")
	if err := flag.Value.Set(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = flag.Value.Set(flag.DefValue)
		flag.Changed = false
	})

	var err error
	out := captureStdout(t, func() { err = runInit(initCmd, nil) })
	return out, err
}

func TestRunInit_CreatesTheTemplateReadableOnlyByItsOwner(t *testing.T) {
	path := filepath.Join(t.TempDir(), "production.yaml")

	out, err := initConfigAt(t, path)
	if err != nil {
		t.Fatalf("runInit() = %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not created: %v", err)
	}
	if string(data) != defaultConfig {
		t.Error("created file differs from the init template")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	for _, want := range []string{"✓ Created " + path, "Next steps:", "tagctl scan"} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
}

func TestRunInit_RefusesToOverwriteAnExistingFile(t *testing.T) {
	const existing = "policy:\n  required:\n    - name: owner\n"
	path := writeFixture(t, t.TempDir(), "tagctl.yaml", existing)

	out, err := initConfigAt(t, path)

	if err == nil || err.Error() != "config file "+path+" already exists" {
		t.Fatalf("runInit() = %v, want the already-exists error", err)
	}
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(data) != existing {
		t.Errorf("existing config was rewritten:\n%s", data)
	}
	if strings.Contains(out, "Created") {
		t.Errorf("output claims a file was created:\n%s", out)
	}
}

func TestRunInit_FailsWhenTheDirectoryDoesNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing", "tagctl.yaml")

	_, err := initConfigAt(t, path)

	if err == nil || !strings.Contains(err.Error(), "failed to create config file") {
		t.Fatalf("runInit() = %v, want the create failure", err)
	}
}
