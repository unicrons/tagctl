package cli

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// validateConfig runs tagctl validate on body and returns its output and error.
func validateConfig(t *testing.T, body string) (string, error) {
	t.Helper()

	path := writeFixture(t, t.TempDir(), "tagctl.yaml", body)
	useConfigFile(t, path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatal(err)
	}

	var err error
	out := captureStdout(t, func() { err = runValidate(validateCmd, nil) })
	return out, err
}

func TestRunValidate_ValidConfigPrintsSummary(t *testing.T) {
	out, err := validateConfig(t, defaultTemplateText(t))
	if err != nil {
		t.Fatalf("runValidate() = %v, want nil\n%s", err, out)
	}

	for _, want := range []string{
		"Validating ",
		"✓ Cloud providers configured: 1\n",
		"✓ Required tags defined: 3\n",
		"✓ Optional tags defined: 2\n",
		"✓ Auto-fix rules defined: 2\n",
		"✓ Ignore rules defined: 2\n",
		"Configuration is valid!\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"Warnings:", "Errors:"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output has a %q section for a clean config:\n%s", unwanted, out)
		}
	}
}

func TestRunValidate_WarningsDoNotFailTheConfig(t *testing.T) {
	body := "clouds:\n  aws:\n    profile: default\n    regions: [us-east-1]\npolicy:\n  required:\n    - name: owner\n"

	out, err := validateConfig(t, body)
	if err != nil {
		t.Fatalf("runValidate() = %v, want nil for a config with warnings only\n%s", err, out)
	}

	for _, want := range []string{
		"Warnings:\n",
		"⚠ clouds.aws should be a list of accounts",
		"Configuration is valid!\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Errors:") {
		t.Errorf("output has an Errors section:\n%s", out)
	}
}

func TestRunValidate_MissingPolicyAndCloudsAreErrors(t *testing.T) {
	out, err := validateConfig(t, "ignore:\n  resources:\n    - \"aws_iam_*\"\n")

	if err == nil || err.Error() != "configuration has 2 error(s)" {
		t.Fatalf("runValidate() = %v, want 2 errors\n%s", err, out)
	}
	for _, want := range []string{
		"✓ Ignore rules defined: 1\n",
		"Errors:\n",
		"✗ no cloud accounts configured\n",
		"✗ no tag policy defined\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output misses %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Configuration is valid!") {
		t.Errorf("output calls a broken config valid:\n%s", out)
	}
}

func TestRunValidate_ReportsWhatConfigLoadRejects(t *testing.T) {
	body := "clouds:\n  aws:\n    - profile: default\n      regions: [eu-west1]\npolicy:\n  required:\n    - name: owner\n"

	out, err := validateConfig(t, body)

	if err == nil || err.Error() != "configuration has 1 error(s)" {
		t.Fatalf("runValidate() = %v, want 1 error\n%s", err, out)
	}
	if !strings.Contains(out, "eu-west1") {
		t.Errorf("output does not name the rejected region:\n%s", out)
	}
}

func TestRunValidate_FailsWithoutAConfigFile(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)

	var err error
	out := captureStdout(t, func() { err = runValidate(validateCmd, nil) })

	if err == nil || !strings.Contains(err.Error(), "no config file found") {
		t.Fatalf("runValidate() = %v, want the missing config error", err)
	}
	if out != "" {
		t.Errorf("printed %q before failing, want nothing", out)
	}
}
