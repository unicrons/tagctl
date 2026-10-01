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
	"unicode"

	"gopkg.in/yaml.v3"
)

// The IAM policies live in permissions/aws/*.json. The CloudFormation
// templates and the docs page embed copies; these tests keep every copy equal
// to the file and the file equal to what the provider actually calls.

const (
	scanPolicyFile      = "tagctl-scan-policy.json"
	applyPolicyFile     = "tagctl-apply-policy.json"
	codeBuildPolicyFile = "tagctl-apply-codebuild-policy.json"
	untagPolicyFile     = "tagctl-apply-untag-policy.json"
)

type policyDocument struct {
	Version   string            `json:"Version" yaml:"Version"`
	Statement []policyStatement `json:"Statement" yaml:"Statement"`
}

type policyStatement struct {
	Sid       string                    `json:"Sid" yaml:"Sid"`
	Effect    string                    `json:"Effect" yaml:"Effect"`
	Action    stringOrList              `json:"Action" yaml:"Action"`
	Resource  stringOrList              `json:"Resource" yaml:"Resource"`
	Condition map[string]map[string]any `json:"Condition,omitempty" yaml:"Condition,omitempty"`
}

// stringOrList is a policy field IAM accepts either as one string or a list.
type stringOrList []string

func (s *stringOrList) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = stringOrList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return err
	}
	*s = many
	return nil
}

func (s *stringOrList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		*s = stringOrList{node.Value}
		return nil
	}
	var many []string
	if err := node.Decode(&many); err != nil {
		return err
	}
	*s = many
	return nil
}

func (s stringOrList) isWildcard() bool {
	return len(s) == 1 && s[0] == "*"
}

// templatePolicy is an inline policy of the template role; condition names
// the Fn::If condition that attaches it, empty when always attached.
type templatePolicy struct {
	condition string
	document  policyDocument
}

type cfnPolicy struct {
	PolicyName     string         `yaml:"PolicyName"`
	PolicyDocument policyDocument `yaml:"PolicyDocument"`
	If             []yaml.Node    `yaml:"Fn::If"`
}

type cfnTemplate struct {
	Resources map[string]struct {
		Type       string `yaml:"Type"`
		Condition  string `yaml:"Condition"`
		Properties struct {
			Policies          []cfnPolicy    `yaml:"Policies"`
			ManagedPolicyArns yaml.Node      `yaml:"ManagedPolicyArns"`
			PolicyDocument    policyDocument `yaml:"PolicyDocument"`
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

func loadTemplatePolicies(t *testing.T, name string) map[string]templatePolicy {
	t.Helper()
	var tpl cfnTemplate
	if err := yaml.Unmarshal(readFile(t, "permissions", "aws", name), &tpl); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	role, ok := tpl.Resources["Role"]
	if !ok || role.Type != "AWS::IAM::Role" {
		t.Fatalf("%s: no AWS::IAM::Role resource named Role", name)
	}
	out := make(map[string]templatePolicy, len(role.Properties.Policies))
	for _, p := range role.Properties.Policies {
		if p.If == nil {
			out[p.PolicyName] = templatePolicy{document: p.PolicyDocument}
			continue
		}
		var inner cfnPolicy
		if len(p.If) != 3 || p.If[1].Decode(&inner) != nil {
			t.Fatalf("%s: Fn::If policy entry must be [condition, policy, AWS::NoValue]", name)
		}
		out[inner.PolicyName] = templatePolicy{condition: p.If[0].Value, document: inner.PolicyDocument}
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

var (
	actionPattern = regexp.MustCompile(`^[a-z0-9-]+:[A-Za-z0-9]+$`)
	// arn:aws:<service>:<region>:<account>:<resource>, service spelled out.
	resourceARNPattern = regexp.MustCompile(`^arn:aws:([a-z0-9-]+):[a-z0-9*-]*:[0-9*]*:.+$`)

	// unscopedWriteActions may stay on "*", with the Service Authorization Reference reason.
	unscopedWriteActions = map[string]string{
		"tag:TagResources":      "lists no resource type",
		"workspaces:CreateTags": "lists no resource type",
		"tag:UntagResources":    "lists no resource type",
		"workspaces:DeleteTags": "lists no resource type",
	}
)

func actionService(action string) string {
	service, _, _ := strings.Cut(action, ":")
	return service
}

func TestPermissionPolicies_WellFormed(t *testing.T) {
	for _, name := range []string{scanPolicyFile, applyPolicyFile, codeBuildPolicyFile, untagPolicyFile} {
		doc := loadPolicyFile(t, name)
		if doc.Version != "2012-10-17" {
			t.Errorf("%s: Version = %q", name, doc.Version)
		}
		seen := map[string]string{}
		for _, st := range doc.Statement {
			if st.Effect != "Allow" {
				t.Errorf("%s/%s: Effect = %q", name, st.Sid, st.Effect)
			}
			services := map[string]bool{}
			for _, a := range st.Action {
				if !actionPattern.MatchString(a) {
					t.Errorf("%s/%s: malformed action %q", name, st.Sid, a)
				}
				if prev, dup := seen[a]; dup {
					t.Errorf("%s: %q listed in both %s and %s", name, a, prev, st.Sid)
				}
				seen[a] = st.Sid
				services[actionService(a)] = true
			}
			if st.Resource.isWildcard() {
				for _, a := range st.Action {
					if _, unscoped := unscopedWriteActions[a]; name != scanPolicyFile && !unscoped {
						t.Errorf("%s/%s: %s is allowed on \"*\"; scope it to the ARNs of the types tagctl tags", name, st.Sid, a)
					}
				}
				continue
			}
			scoped := map[string]bool{}
			for _, r := range st.Resource {
				m := resourceARNPattern.FindStringSubmatch(r)
				if m == nil {
					t.Errorf("%s/%s: malformed Resource %q", name, st.Sid, r)
					continue
				}
				if !services[m[1]] {
					t.Errorf("%s/%s: Resource %q matches no action of the statement", name, st.Sid, r)
				}
				scoped[m[1]] = true
			}
			for _, a := range st.Action {
				if !scoped[actionService(a)] {
					t.Errorf("%s/%s: %s has no Resource in its service namespace, IAM would deny it", name, st.Sid, a)
				}
			}
		}
	}
}

var writeAction = regexp.MustCompile(`^[a-z0-9-]+:(Tag|Untag|Add|Create|Put|Change|Update|Delete)`)

func TestPermissionPolicies_ScanIsReadOnly(t *testing.T) {
	scan := loadPolicyFile(t, scanPolicyFile)
	for a := range actionSet(scan) {
		if writeAction.MatchString(a) {
			t.Errorf("scan policy grants a write action: %s", a)
		}
	}
	if !actionSet(scan)["tag:GetResources"] {
		t.Error("scan policy lacks tag:GetResources, the bulk tag sweep")
	}
	apply := loadPolicyFile(t, applyPolicyFile)
	if !actionSet(apply)["tag:TagResources"] {
		t.Error("apply policy lacks tag:TagResources, the Tagging API fallback")
	}
	for a := range actionSet(apply, loadPolicyFile(t, codeBuildPolicyFile), loadPolicyFile(t, untagPolicyFile)) {
		if actionSet(scan)[a] {
			t.Errorf("%s is in both policies; the apply role already attaches the scan policy", a)
		}
	}
}

func TestPermissionPolicies_CodeBuildTaggingIsOptIn(t *testing.T) {
	if actionSet(loadPolicyFile(t, applyPolicyFile))["codebuild:UpdateProject"] {
		t.Error("the default apply policy grants codebuild:UpdateProject, which can rewrite a project's buildspec")
	}
	if got := actionSet(loadPolicyFile(t, codeBuildPolicyFile)); !reflect.DeepEqual(got, map[string]bool{"codebuild:UpdateProject": true}) {
		t.Errorf("%s actions = %v, want only codebuild:UpdateProject", codeBuildPolicyFile, got)
	}
}

func TestPermissionPolicies_TemplatesMatchFiles(t *testing.T) {
	scan := loadPolicyFile(t, scanPolicyFile)
	apply := loadPolicyFile(t, applyPolicyFile)
	codeBuild := loadPolicyFile(t, codeBuildPolicyFile)

	want := map[string]map[string]templatePolicy{
		"tagctl-scan-role.yaml": {"TagctlScan": {document: scan}},
		"tagctl-apply-role.yaml": {
			"TagctlScan":           {document: scan},
			"TagctlApply":          {document: apply},
			"TagctlApplyCodeBuild": {condition: "CodeBuildTagging", document: codeBuild},
		},
	}
	for name, policies := range want {
		got := loadTemplatePolicies(t, name)
		if name == "tagctl-apply-role.yaml" {
			checkProtectedTagKeysPolicy(t, got["TagctlProtectedTagKeys"])
			delete(got, "TagctlProtectedTagKeys")
		}
		if !reflect.DeepEqual(got, policies) {
			t.Errorf("%s: inline policies differ from permissions/aws/*.json; regenerate the template", name)
		}
	}
}

var removeAction = regexp.MustCompile(`^[a-z0-9-]+:(Untag|Remove|Delete|DELETE)`)

func TestPermissionPolicies_TagRemovalIsOptIn(t *testing.T) {
	for a := range actionSet(loadPolicyFile(t, applyPolicyFile)) {
		if removeAction.MatchString(a) {
			t.Errorf("the default apply policy grants %s; removals belong in %s", a, untagPolicyFile)
		}
	}
	untag := loadPolicyFile(t, untagPolicyFile)
	for a := range actionSet(untag) {
		if !removeAction.MatchString(a) {
			t.Errorf("%s grants %s, which is not a tag removal", untagPolicyFile, a)
		}
	}
	for _, a := range []string{"tag:UntagResources", "ec2:DeleteTags"} {
		if !actionSet(untag)[a] {
			t.Errorf("%s lacks %s", untagPolicyFile, a)
		}
	}

	var tpl cfnTemplate
	if err := yaml.Unmarshal(readFile(t, "permissions", "aws", "tagctl-apply-role.yaml"), &tpl); err != nil {
		t.Fatal(err)
	}
	managed := tpl.Resources["UntagPolicy"]
	if managed.Type != "AWS::IAM::ManagedPolicy" || managed.Condition != "TagRemoval" {
		t.Fatalf("UntagPolicy = %s under condition %q, want a managed policy under TagRemoval", managed.Type, managed.Condition)
	}
	if !reflect.DeepEqual(managed.Properties.PolicyDocument, untag) {
		t.Errorf("UntagPolicy differs from permissions/aws/%s; regenerate the template", untagPolicyFile)
	}
	var attach struct {
		If []yaml.Node `yaml:"Fn::If"`
	}
	arns := tpl.Resources["Role"].Properties.ManagedPolicyArns
	if err := arns.Decode(&attach); err != nil || len(attach.If) != 3 || attach.If[0].Value != "TagRemoval" {
		t.Errorf("Role.ManagedPolicyArns must attach UntagPolicy only under TagRemoval")
	}

	size := 0
	for _, r := range string(readFile(t, "permissions", "aws", untagPolicyFile)) {
		if !unicode.IsSpace(r) {
			size++
		}
	}
	if size > managedPolicyLimit {
		t.Errorf("%s uses %d characters; IAM allows %d in a managed policy", untagPolicyFile, size, managedPolicyLimit)
	}
}

func checkProtectedTagKeysPolicy(t *testing.T, p templatePolicy) {
	t.Helper()
	if p.condition != "HasProtectedTagKeys" || len(p.document.Statement) != 1 {
		t.Fatalf("TagctlProtectedTagKeys must be one statement attached under HasProtectedTagKeys, got %+v", p)
	}
	st := p.document.Statement[0]
	if st.Effect != "Deny" || !st.Action.isWildcard() || !st.Resource.isWildcard() {
		t.Errorf("TagctlProtectedTagKeys = %+v, want Deny on every action and resource", st)
	}
	if _, ok := st.Condition["ForAnyValue:StringLike"]["aws:TagKeys"]; !ok {
		t.Errorf("TagctlProtectedTagKeys condition = %v, want ForAnyValue:StringLike on aws:TagKeys", st.Condition)
	}
}

func TestPermissionPolicies_DocsMatchFiles(t *testing.T) {
	cases := map[string]string{
		"### Read-Only (Scan)":           scanPolicyFile,
		"### Read-Write (Apply)":         applyPolicyFile,
		"### CodeBuild Tagging (Opt-In)": codeBuildPolicyFile,
		"### Tag Removal (Opt-In)":       untagPolicyFile,
	}
	for heading, file := range cases {
		if got, want := loadDocsPolicy(t, heading), loadPolicyFile(t, file); !reflect.DeepEqual(got, want) {
			t.Errorf("docs/providers/aws.mdx %q differs from permissions/aws/%s", heading, file)
		}
	}
}

const (
	// roleInlinePolicyLimit is IAM's cap on the combined inline policies of a
	// role, whitespace excluded.
	roleInlinePolicyLimit = 10240
	// protectedTagKeysReserve keeps room for the template-only deny.
	protectedTagKeysReserve = 768
	// managedPolicyLimit is IAM's cap on one managed policy, whitespace excluded.
	managedPolicyLimit = 6144
)

func TestPermissionPolicies_ApplyRoleFitsInlineLimit(t *testing.T) {
	size := 0
	for _, name := range []string{scanPolicyFile, applyPolicyFile, codeBuildPolicyFile} {
		for _, r := range string(readFile(t, "permissions", "aws", name)) {
			if !unicode.IsSpace(r) {
				size++
			}
		}
	}
	if size > roleInlinePolicyLimit-protectedTagKeysReserve {
		t.Errorf("apply role inline policies use %d characters; IAM allows %d and %d are kept for TagctlProtectedTagKeys",
			size, roleInlinePolicyLimit, protectedTagKeysReserve)
	}
}

var (
	paginatorCall = regexp.MustCompile(`New([A-Z][A-Za-z0-9]+)Paginator\(`)
	directCall    = regexp.MustCompile(`\.((?:List|Describe|Get|Batch|Tag|Untag|Add|Create|Put|Change|Delete)[A-Za-z0-9]*)\(ctx`)

	// SDK operations whose IAM action has a different name.
	actionAliases = map[string]string{
		"GetApis":     "GET",
		"GetRestApis": "GET",
		"ListBuckets": "ListAllMyBuckets",
		// S3 authorizes dropping the tag set with the action that writes it.
		"DeleteBucketTagging": "PutBucketTagging",
	}
	// Exported methods of this package that the regexp would mistake for SDK calls.
	notSDKCalls = map[string]bool{"ListResources": true}
)

// Every SDK operation the provider calls must be allowed by one of the
// policies, so adding a lister without extending the policy fails here.
func TestPermissionPolicies_CoverProviderCalls(t *testing.T) {
	allowed := actionSet(loadPolicyFile(t, scanPolicyFile), loadPolicyFile(t, applyPolicyFile),
		loadPolicyFile(t, codeBuildPolicyFile), loadPolicyFile(t, untagPolicyFile))
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
