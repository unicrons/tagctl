package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/unicrons/tagctl/internal/config"
)

func TestConfigTemplates_MatchEmbeddedFiles(t *testing.T) {
	entries, err := configTemplateFS.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	embedded := make([]string, 0, len(entries))
	for _, entry := range entries {
		embedded = append(embedded, strings.TrimSuffix(entry.Name(), ".yaml"))
	}
	listed := templateNames()
	slices.Sort(listed)

	if !slices.Equal(embedded, listed) {
		t.Errorf("embedded templates = %v, listed templates = %v", embedded, listed)
	}
}

func TestInit_EveryTemplateLoadsAndValidates(t *testing.T) {
	for _, name := range templateNames() {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tagctl.yaml")

			if run := execute(t, "init", "--template", name, "--name", path); run.err != nil {
				t.Fatalf("init --template %s error = %v", name, run.err)
			}

			cfg, err := config.Load(path)
			if err != nil {
				t.Fatalf("config.Load(%s template) = %v", name, err)
			}
			if len(cfg.Clouds.AWS) == 0 || len(cfg.Policy.Required) == 0 {
				t.Errorf("%s template has %d AWS accounts and %d required tags, want both set",
					name, len(cfg.Clouds.AWS), len(cfg.Policy.Required))
			}

			run := execute(t, "-c", path, "validate")
			if run.err != nil {
				t.Fatalf("validate error = %v\n%s", run.err, run.stdout)
			}
			if !strings.Contains(run.stdout, "Configuration is valid!") || strings.Contains(run.stdout, "Warnings:") {
				t.Errorf("validate did not accept the %s template cleanly:\n%s", name, run.stdout)
			}
		})
	}
}

func TestInit_TemplatesRequireTheirTags(t *testing.T) {
	tests := map[string][]string{
		"finops":           {"cost-center", "owner", "environment", "project"},
		"security":         {"data-classification", "owner", "environment", "compliance"},
		"well-architected": {"application", "environment", "owner", "cost-center", "confidentiality", "compliance"},
	}

	for name, want := range tests {
		t.Run(name, func(t *testing.T) {
			content, err := readTemplate(name)
			if err != nil {
				t.Fatal(err)
			}
			path := writeFixture(t, t.TempDir(), "tagctl.yaml", string(content))
			cfg, err := config.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			required := make([]string, 0, len(cfg.Policy.Required))
			for _, req := range cfg.Policy.Required {
				required = append(required, req.Name)
			}

			for _, tag := range want {
				if !slices.Contains(required, tag) {
					t.Errorf("%s template requires %v, want %q among them", name, required, tag)
				}
			}
		})
	}
}

func TestInit_WithoutTemplateWritesTheDefaultOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")
	want, err := readTemplate(defaultTemplate)
	if err != nil {
		t.Fatal(err)
	}

	if run := execute(t, "init", "--name", path); run.err != nil {
		t.Fatalf("init error = %v", run.err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Errorf("init wrote a config that is not the default template:\n%s", got)
	}
}

func TestInit_UnknownTemplateListsTheValidNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")

	run := execute(t, "init", "--template", "nosuch", "--name", path)

	if run.err == nil {
		t.Fatal("init --template nosuch succeeded")
	}
	for _, want := range append([]string{`"nosuch"`}, templateNames()...) {
		if !strings.Contains(run.err.Error(), want) {
			t.Errorf("error %q does not mention %s", run.err, want)
		}
	}
	if code := ExitCode(run.err); code != exitError {
		t.Errorf("exit code = %d, want %d", code, exitError)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("init wrote a config file for an unknown template")
	}
}

func TestInit_ListTemplatesPrintsEveryTemplateAndWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tagctl.yaml")

	run := execute(t, "init", "--list-templates", "--name", path)

	if run.err != nil {
		t.Fatalf("init --list-templates error = %v", run.err)
	}
	lines := strings.Split(strings.TrimSpace(run.stdout), "\n")
	if len(lines) != len(configTemplates) {
		t.Fatalf("stdout has %d lines, want one per template:\n%s", len(lines), run.stdout)
	}
	for i, tmpl := range configTemplates {
		if !strings.HasPrefix(lines[i], tmpl.name+" ") || !strings.Contains(lines[i], tmpl.description) {
			t.Errorf("line %d = %q, want %s and its description", i, lines[i], tmpl.name)
		}
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("init --list-templates wrote a config file")
	}
}
