package aws

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// The IAM policies live in permissions/aws/*.json. The CloudFormation
// templates and the docs page embed copies; these tests keep every copy equal
// to the file and the file equal to what the provider actually calls.

type policyDocument struct {
	Version   string            `json:"Version" yaml:"Version"`
	Statement []policyStatement `json:"Statement" yaml:"Statement"`
}

type policyStatement struct {
	Sid      string   `json:"Sid" yaml:"Sid"`
	Effect   string   `json:"Effect" yaml:"Effect"`
	Action   []string `json:"Action" yaml:"Action"`
	Resource string   `json:"Resource" yaml:"Resource"`
}

type cfnTemplate struct {
	Resources map[string]struct {
		Type       string `yaml:"Type"`
		Properties struct {
			Policies []struct {
				PolicyName     string         `yaml:"PolicyName"`
				PolicyDocument policyDocument `yaml:"PolicyDocument"`
			} `yaml:"Policies"`
		} `yaml:"Properties"`
	} `yaml:"Resources"`
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate test file")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..")
}

func readFile(t *testing.T, parts ...string) []byte {
	t.Helper()
	path := filepath.Join(append([]string{repoRoot(t)}, parts...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return data
}

func loadPolicyFile(t *testing.T, name string) policyDocument {
	t.Helper()
	var doc policyDocument
	if err := json.Unmarshal(readFile(t, "permissions", "aws", name), &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func loadTemplatePolicies(t *testing.T, name string) map[string]policyDocument {
	t.Helper()
	var tpl cfnTemplate
	if err := yaml.Unmarshal(readFile(t, "permissions", "aws", name), &tpl); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	role, ok := tpl.Resources["Role"]
	if !ok || role.Type != "AWS::IAM::Role" {
		t.Fatalf("%s: no AWS::IAM::Role resource named Role", name)
	}
	out := make(map[string]policyDocument, len(role.Properties.Policies))
	for _, p := range role.Properties.Policies {
		out[p.PolicyName] = p.PolicyDocument
	}
	return out
}

// loadDocsPolicy returns the first ```json block after the given heading in
// docs/providers/aws.mdx.
func loadDocsPolicy(t *testing.T, heading string) policyDocument {
	t.Helper()
	page := string(readFile(t, "docs", "providers", "aws.mdx"))
	start := strings.Index(page, "\n"+heading+"\n")
	if start < 0 {
		t.Fatalf("docs/providers/aws.mdx: heading %q not found", heading)
	}
	rest := page[start:]
	open := strings.Index(rest, "```json\n")
	if open < 0 {
		t.Fatalf("docs/providers/aws.mdx: no json block after %q", heading)
	}
	rest = rest[open+len("```json\n"):]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		t.Fatalf("docs/providers/aws.mdx: unterminated json block after %q", heading)
	}
	var doc policyDocument
	if err := json.Unmarshal([]byte(rest[:end]), &doc); err != nil {
		t.Fatalf("docs/providers/aws.mdx: json block after %q: %v", heading, err)
	}
	return doc
}

func actionSet(docs ...policyDocument) map[string]bool {
	set := map[string]bool{}
	for _, doc := range docs {
		for _, st := range doc.Statement {
			for _, a := range st.Action {
				set[a] = true
			}
		}
	}
	return set
}

var actionPattern = regexp.MustCompile(`^[a-z0-9-]+:[A-Za-z0-9]+$`)

func TestPermissionPolicies_WellFormed(t *testing.T) {
	for _, name := range []string{"tagctl-scan-policy.json", "tagctl-apply-policy.json"} {
		doc := loadPolicyFile(t, name)
		if doc.Version != "2012-10-17" {
			t.Errorf("%s: Version = %q", name, doc.Version)
		}
		seen := map[string]string{}
		for _, st := range doc.Statement {
			if st.Effect != "Allow" {
				t.Errorf("%s/%s: Effect = %q", name, st.Sid, st.Effect)
			}
			if st.Resource != "*" {
				t.Errorf("%s/%s: Resource = %q, the docs tell users to scope it themselves", name, st.Sid, st.Resource)
			}
			for _, a := range st.Action {
				if !actionPattern.MatchString(a) {
					t.Errorf("%s/%s: malformed action %q", name, st.Sid, a)
				}
				if prev, dup := seen[a]; dup {
					t.Errorf("%s: %q listed in both %s and %s", name, a, prev, st.Sid)
				}
				seen[a] = st.Sid
			}
		}
	}
}

var writeAction = regexp.MustCompile(`^[a-z0-9-]+:(Tag|Untag|Add|Create|Put|Change|Update|Delete)`)

func TestPermissionPolicies_ScanIsReadOnly(t *testing.T) {
	scan := loadPolicyFile(t, "tagctl-scan-policy.json")
	for a := range actionSet(scan) {
		if writeAction.MatchString(a) {
			t.Errorf("scan policy grants a write action: %s", a)
		}
	}
	if !actionSet(scan)["tag:GetResources"] {
		t.Error("scan policy lacks tag:GetResources, the bulk tag sweep")
	}
	apply := loadPolicyFile(t, "tagctl-apply-policy.json")
	if !actionSet(apply)["tag:TagResources"] {
		t.Error("apply policy lacks tag:TagResources, the Tagging API fallback")
	}
	for a := range actionSet(apply) {
		if actionSet(scan)[a] {
			t.Errorf("%s is in both policies; the apply role already attaches the scan policy", a)
		}
	}
}

func TestPermissionPolicies_TemplatesMatchFiles(t *testing.T) {
	scan := loadPolicyFile(t, "tagctl-scan-policy.json")
	apply := loadPolicyFile(t, "tagctl-apply-policy.json")

	want := map[string]map[string]policyDocument{
		"tagctl-scan-role.yaml":  {"TagctlScan": scan},
		"tagctl-apply-role.yaml": {"TagctlScan": scan, "TagctlApply": apply},
	}
	for name, policies := range want {
		got := loadTemplatePolicies(t, name)
		if !reflect.DeepEqual(got, policies) {
			t.Errorf("%s: inline policies differ from permissions/aws/*.json; regenerate the template", name)
		}
	}
}

func TestPermissionPolicies_DocsMatchFiles(t *testing.T) {
	cases := map[string]string{
		"### Read-Only (Scan)":   "tagctl-scan-policy.json",
		"### Read-Write (Apply)": "tagctl-apply-policy.json",
	}
	for heading, file := range cases {
		if got, want := loadDocsPolicy(t, heading), loadPolicyFile(t, file); !reflect.DeepEqual(got, want) {
			t.Errorf("docs/providers/aws.mdx %q differs from permissions/aws/%s", heading, file)
		}
	}
}

var (
	paginatorCall = regexp.MustCompile(`New([A-Z][A-Za-z0-9]+)Paginator\(`)
	directCall    = regexp.MustCompile(`\.((?:List|Describe|Get|Batch|Tag|Untag|Add|Create|Put|Change)[A-Za-z0-9]*)\(ctx`)

	// SDK operations whose IAM action has a different name.
	actionAliases = map[string]string{
		"GetApis":     "GET",
		"GetRestApis": "GET",
		"ListBuckets": "ListAllMyBuckets",
	}
	// Exported methods of this package that the regexp would mistake for SDK calls.
	notSDKCalls = map[string]bool{"ListResources": true}
)

// Every SDK operation the provider calls must be allowed by one of the two
// policies, so adding a lister without extending the policy fails here.
func TestPermissionPolicies_CoverProviderCalls(t *testing.T) {
	allowed := actionSet(loadPolicyFile(t, "tagctl-scan-policy.json"), loadPolicyFile(t, "tagctl-apply-policy.json"))
	suffixes := map[string]bool{}
	for a := range allowed {
		suffixes[a[strings.Index(a, ":")+1:]] = true
	}

	_, file, _, _ := runtime.Caller(0)
	sources, err := filepath.Glob(filepath.Join(filepath.Dir(file), "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	missing := map[string][]string{}
	for _, src := range sources {
		if strings.HasSuffix(src, "_test.go") {
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		ops := map[string]bool{}
		for _, m := range paginatorCall.FindAllStringSubmatch(string(data), -1) {
			ops[m[1]] = true
		}
		for _, m := range directCall.FindAllStringSubmatch(string(data), -1) {
			ops[m[1]] = true
		}
		for op := range ops {
			if notSDKCalls[op] {
				continue
			}
			if alias, ok := actionAliases[op]; ok {
				op = alias
			}
			if !suffixes[op] {
				missing[filepath.Base(src)] = append(missing[filepath.Base(src)], op)
			}
		}
	}
	for src, ops := range missing {
		sort.Strings(ops)
		t.Errorf("%s calls %v but no policy in permissions/aws allows them", src, ops)
	}
}
