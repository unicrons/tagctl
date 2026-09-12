// Package config handles parsing and validation of tagctl configuration.
package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config represents the complete tagctl configuration.
type Config struct {
	// Clouds defines the cloud accounts to scan.
	Clouds CloudsConfig `yaml:"clouds" mapstructure:"clouds"`

	// Policy defines the tag requirements.
	Policy PolicyConfig `yaml:"policy" mapstructure:"policy"`

	// Rules defines auto-fix rules.
	Rules RulesConfig `yaml:"rules" mapstructure:"rules"`

	// Ignore defines resources to skip.
	Ignore IgnoreConfig `yaml:"ignore" mapstructure:"ignore"`

	// Normalize configures tag value drift detection.
	Normalize NormalizeConfig `yaml:"normalize" mapstructure:"normalize"`
}

// NormalizeConfig controls how tagctl groups tag values that look like
// variants of one another.
type NormalizeConfig struct {
	// MaxDistance is the edit distance under which two values are treated as
	// a typo of each other. 0 disables typo matching. Defaults to 1.
	MaxDistance *int `yaml:"max_distance" mapstructure:"max_distance"`

	// MatchAbbreviations groups a value with a longer one it is a prefix of,
	// which is what catches "prod" against "production". Defaults to true.
	MatchAbbreviations *bool `yaml:"match_abbreviations" mapstructure:"match_abbreviations"`

	// IgnoreTags are tag keys to leave alone, on top of the Name tag which is
	// always ignored because its values are unique by design.
	IgnoreTags []string `yaml:"ignore_tags" mapstructure:"ignore_tags"`
}

// CloudsConfig contains cloud provider configurations.
type CloudsConfig struct {
	AWS        AWSAccounts         `yaml:"aws" mapstructure:"aws"`
	GCP        []GCPAccount        `yaml:"gcp" mapstructure:"gcp"`
	Azure      []AzureAccount      `yaml:"azure" mapstructure:"azure"`
	Kubernetes []KubernetesCluster `yaml:"kubernetes" mapstructure:"kubernetes"`
}

// KubernetesCluster represents a Kubernetes cluster configuration.
type KubernetesCluster struct {
	// Name is a friendly name for this cluster.
	Name string `yaml:"name" mapstructure:"name"`

	// Kubeconfig is the path to kubeconfig file or "in-cluster" for in-cluster config.
	Kubeconfig string `yaml:"kubeconfig" mapstructure:"kubeconfig"`

	// Context is the kubeconfig context to use (optional).
	Context string `yaml:"context" mapstructure:"context"`

	// Namespaces to scan. Empty list means all namespaces.
	Namespaces []string `yaml:"namespaces" mapstructure:"namespaces"`

	// ResourceTypes to scan. Empty list means KubernetesDefaultResourceTypes.
	ResourceTypes []string `yaml:"resource_types" mapstructure:"resource_types"`
}

// Kubernetes resource types accepted in resource_types.
const (
	KubernetesPod        = "k8s_pod"
	KubernetesDeployment = "k8s_deployment"
	KubernetesService    = "k8s_service"
	KubernetesNamespace  = "k8s_namespace"
	KubernetesConfigMap  = "k8s_configmap"
	KubernetesSecret     = "k8s_secret"
)

// KubernetesResourceTypes are every value resource_types accepts.
var KubernetesResourceTypes = []string{
	KubernetesPod,
	KubernetesDeployment,
	KubernetesService,
	KubernetesNamespace,
	KubernetesConfigMap,
	KubernetesSecret,
}

// KubernetesDefaultResourceTypes are scanned when resource_types is empty.
// Secrets are left out: they are scanned only when listed explicitly.
var KubernetesDefaultResourceTypes = []string{
	KubernetesPod,
	KubernetesDeployment,
	KubernetesService,
	KubernetesNamespace,
	KubernetesConfigMap,
}

func (k KubernetesCluster) validateResourceTypes(i int) error {
	seen := make(map[string]bool, len(k.ResourceTypes))
	for _, rt := range k.ResourceTypes {
		if !slices.Contains(KubernetesResourceTypes, rt) {
			return fmt.Errorf("kubernetes[%d]: unknown resource type %q (supported: %s)",
				i, rt, strings.Join(KubernetesResourceTypes, ", "))
		}
		if seen[rt] {
			return fmt.Errorf("kubernetes[%d]: resource type %q listed twice", i, rt)
		}
		seen[rt] = true
	}
	return nil
}

// AWSAccounts is the clouds.aws list of accounts.
type AWSAccounts []AWSAccount

// UnmarshalYAML also accepts a single account written as a mapping, which
// tagctl validate warns about.
func (a *AWSAccounts) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.MappingNode {
		node = &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{node}}
	}
	return node.Decode((*[]AWSAccount)(a))
}

// AWSAccount represents an AWS account configuration.
//
// Credentials come from the SDK default chain (environment, shared config,
// SSO, container or instance role); Profile selects a shared config profile
// and RoleARN assumes a role on top of whatever the chain resolved.
type AWSAccount struct {
	// Profile is the AWS CLI profile name.
	Profile string `yaml:"profile" mapstructure:"profile"`

	// RoleARN is an IAM role to assume with the resolved credentials.
	RoleARN string `yaml:"role_arn" mapstructure:"role_arn"`

	// ExternalID is passed to AssumeRole when the role requires one.
	ExternalID string `yaml:"external_id" mapstructure:"external_id"`

	// SessionDuration of the assumed role in seconds (900 to 43200, default 3600).
	SessionDuration int `yaml:"session_duration" mapstructure:"session_duration"`

	// RoleSessionName identifies the assumed-role session in CloudTrail.
	RoleSessionName string `yaml:"role_session_name" mapstructure:"role_session_name"`

	// MFASerial is the MFA device ARN when the role requires MFA; the code is
	// read from stdin.
	MFASerial string `yaml:"mfa_serial" mapstructure:"mfa_serial"`

	// Regions to scan.
	Regions []string `yaml:"regions" mapstructure:"regions"`

	// Unknown collects keys that are not part of the schema.
	Unknown map[string]any `yaml:",inline" mapstructure:",remain"`
}

// GCPAccount represents a GCP project configuration.
type GCPAccount struct {
	// Project is the GCP project ID.
	Project string `yaml:"project" mapstructure:"project"`

	// CredentialsFile path to service account JSON.
	CredentialsFile string `yaml:"credentials_file" mapstructure:"credentials_file"`
}

// AzureAccount represents an Azure subscription configuration.
type AzureAccount struct {
	// Subscription ID.
	Subscription string `yaml:"subscription" mapstructure:"subscription"`
}

// PolicyConfig defines tag requirements.
type PolicyConfig struct {
	// Required tags that must be present.
	Required []TagRequirement `yaml:"required" mapstructure:"required"`

	// Optional tags that are tracked but not required.
	Optional []TagRequirement `yaml:"optional" mapstructure:"optional"`
}

// TagRequirement defines a tag policy.
type TagRequirement struct {
	// Name is the tag key.
	Name string `yaml:"name" mapstructure:"name"`

	// Description explains what this tag is for.
	Description string `yaml:"description" mapstructure:"description"`

	// Values is a list of allowed values, checked before Pattern.
	Values []string `yaml:"values" mapstructure:"values"`

	// Pattern is a regex the value must match; when Values is also set the
	// value must satisfy both.
	Pattern string `yaml:"pattern" mapstructure:"pattern"`
}

// RulesConfig defines auto-fix rules.
type RulesConfig struct {
	// Infer rules derive tag values from resource names.
	Infer []InferRule `yaml:"infer" mapstructure:"infer"`

	// Inherit rules copy tags from parent resources.
	Inherit []InheritRule `yaml:"inherit" mapstructure:"inherit"`

	// Defaults rules set default values.
	Defaults []DefaultRule `yaml:"defaults" mapstructure:"defaults"`
}

// InferRule infers tag values from resource naming conventions.
type InferRule struct {
	// Tag is the tag to infer.
	Tag string `yaml:"tag" mapstructure:"tag"`

	// FromName contains patterns to match against resource names.
	FromName []NamePattern `yaml:"from_name" mapstructure:"from_name"`
}

// NamePattern matches resource names and maps to values.
type NamePattern struct {
	// Pattern is the regex to match.
	Pattern string `yaml:"pattern" mapstructure:"pattern"`

	// Value is the tag value to set when pattern matches.
	Value string `yaml:"value" mapstructure:"value"`
}

// InheritRule copies tags from parent resources.
type InheritRule struct {
	// Resource is the resource type to apply this rule to.
	Resource string `yaml:"resource" mapstructure:"resource"`

	// From is the parent resource relationship.
	From string `yaml:"from" mapstructure:"from"`

	// Tags to inherit from the parent.
	Tags []string `yaml:"tags" mapstructure:"tags"`
}

// DefaultRule sets default tag values.
type DefaultRule struct {
	// Resource is a glob pattern for resource types.
	Resource string `yaml:"resource" mapstructure:"resource"`

	// When conditions must all be true to apply: "tag:<name>" keys whose value
	// is ConditionAbsent or the exact tag value.
	When map[string]string `yaml:"when" mapstructure:"when"`

	// Set are the tag values to set.
	Set map[string]string `yaml:"set" mapstructure:"set"`
}

// IgnoreConfig defines resources to skip during scanning.
type IgnoreConfig struct {
	// Resources is a list of glob patterns for resource types to ignore.
	Resources []string `yaml:"resources" mapstructure:"resources"`

	// Tags defines tag values that mark resources to ignore.
	Tags map[string][]string `yaml:"tags" mapstructure:"tags"`
}

// Condition keys and values understood in rules.defaults[].when.
const (
	ConditionTagPrefix = "tag:"
	ConditionAbsent    = "absent"
	ConditionPresent   = "present" // reserved: rejected until it is a condition
)

// Load reads a configuration file, rejects keys outside the schema and
// validates the result. An empty file is an empty configuration.
func Load(path string) (*Config, error) {
	// #nosec G304 -- the config path is supplied by the user running the CLI.
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("failed to parse config file %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config file %s: %w", path, err)
	}

	return &cfg, nil
}

// awsStaticKeys are the fields tagctl used to read from the file; the SDK
// shared config and the environment are the only credential sources now.
var awsStaticKeys = []string{"access_key_id", "secret_access_key", "session_token"}

func (a AWSAccount) validateKeys(i int) error {
	for _, key := range awsStaticKeys {
		if _, ok := a.Unknown[key]; ok {
			return fmt.Errorf("aws[%d]: %s is not read from the config file; put credentials in "+
				"~/.aws/credentials or the environment (AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, AWS_SESSION_TOKEN)", i, key)
		}
	}
	for key := range a.Unknown {
		return fmt.Errorf("aws[%d]: unknown field %q", i, key)
	}
	return nil
}

func (a AWSAccount) validateRole(i int) error {
	if a.RoleARN == "" {
		switch {
		case a.ExternalID != "":
			return fmt.Errorf("aws[%d]: external_id requires role_arn", i)
		case a.SessionDuration != 0:
			return fmt.Errorf("aws[%d]: session_duration requires role_arn", i)
		case a.RoleSessionName != "":
			return fmt.Errorf("aws[%d]: role_session_name requires role_arn", i)
		case a.MFASerial != "":
			return fmt.Errorf("aws[%d]: mfa_serial requires role_arn", i)
		}
		return nil
	}
	if !awsRoleARNPattern.MatchString(a.RoleARN) {
		return fmt.Errorf("aws[%d]: invalid role_arn %q", i, a.RoleARN)
	}
	if a.SessionDuration != 0 && (a.SessionDuration < minSessionDuration || a.SessionDuration > maxSessionDuration) {
		return fmt.Errorf("aws[%d]: session_duration must be between %d and %d seconds", i, minSessionDuration, maxSessionDuration)
	}
	if a.MFASerial != "" && !awsMFASerialPattern.MatchString(a.MFASerial) {
		return fmt.Errorf("aws[%d]: invalid mfa_serial %q", i, a.MFASerial)
	}
	return nil
}

// AssumeRole limits imposed by STS.
const (
	minSessionDuration = 900
	maxSessionDuration = 43200
)

var (
	awsRoleARNPattern   = regexp.MustCompile(`^arn:aws[a-z-]*:iam::\d{12}:role/[\w+=,.@/-]+$`)
	awsMFASerialPattern = regexp.MustCompile(`^arn:aws[a-z-]*:iam::\d{12}:mfa/[\w+=,.@/-]+$`)
)

// awsRegionPattern matches region codes such as us-east-1, eu-west-2,
// us-gov-west-1 or ap-southeast-3. It catches typos, not unknown regions.
var awsRegionPattern = regexp.MustCompile(`^[a-z]{2}(-gov|-iso[a-z]?)?-[a-z]+-\d+$`)

// Validate checks the configuration for errors.
//
// It does not require a cloud provider: scan runs in demo mode without one
// and evaluate works from a JSON file.
func (c *Config) Validate() error {
	if err := c.validateAWS(); err != nil {
		return err
	}

	for i, k8s := range c.Clouds.Kubernetes {
		if k8s.Name == "" {
			return fmt.Errorf("kubernetes[%d]: name is required", i)
		}
		if err := k8s.validateResourceTypes(i); err != nil {
			return err
		}
	}

	if err := c.Policy.validate(); err != nil {
		return err
	}
	if err := c.Rules.validate(); err != nil {
		return err
	}
	return c.Ignore.validate()
}

// validate checks every tag requirement and rejects a tag defined twice.
func (p PolicyConfig) validate() error {
	sections := []struct {
		field string
		reqs  []TagRequirement
	}{
		{"policy.required", p.Required},
		{"policy.optional", p.Optional},
	}
	definedAt := make(map[string]string, len(p.Required)+len(p.Optional))
	for _, section := range sections {
		for i, req := range section.reqs {
			at := fmt.Sprintf("%s[%d]", section.field, i)
			if req.Name == "" {
				return fmt.Errorf("%s: name is required", at)
			}
			if first, ok := definedAt[req.Name]; ok {
				return fmt.Errorf("%s: tag %q is already defined at %s", at, req.Name, first)
			}
			definedAt[req.Name] = at
			if req.Pattern != "" {
				if _, err := regexp.Compile(req.Pattern); err != nil {
					return fmt.Errorf("%s: invalid pattern %q: %w", at, req.Pattern, err)
				}
			}
		}
	}
	return nil
}

func (r RulesConfig) validate() error {
	for i, rule := range r.Infer {
		for j, pattern := range rule.FromName {
			if _, err := regexp.Compile(pattern.Pattern); err != nil {
				return fmt.Errorf("rules.infer[%d].from_name[%d]: invalid pattern %q: %w", i, j, pattern.Pattern, err)
			}
		}
	}
	for i, rule := range r.Defaults {
		if err := rule.validate(); err != nil {
			return fmt.Errorf("rules.defaults[%d]: %w", i, err)
		}
	}
	return nil
}

func (d DefaultRule) validate() error {
	if d.Resource == "" {
		return errors.New("resource is required")
	}
	if err := validateGlob(d.Resource); err != nil {
		return fmt.Errorf("resource: %w", err)
	}
	if len(d.Set) == 0 {
		return errors.New("set must name at least one tag")
	}
	for _, key := range slices.Sorted(maps.Keys(d.When)) {
		if tag, ok := strings.CutPrefix(key, ConditionTagPrefix); !ok || tag == "" {
			return fmt.Errorf("when: unknown condition %q, expected %s<name>", key, ConditionTagPrefix)
		}
		if value := d.When[key]; value == "" || value == ConditionPresent {
			return fmt.Errorf("when: %s must be %q or the exact tag value, got %q", key, ConditionAbsent, value)
		}
	}
	return nil
}

func (ig IgnoreConfig) validate() error {
	for i, pattern := range ig.Resources {
		if err := validateGlob(pattern); err != nil {
			return fmt.Errorf("ignore.resources[%d]: %w", i, err)
		}
	}
	return nil
}

// validateGlob rejects resource type patterns that path.Match, which the
// engine matches them with, cannot parse.
func validateGlob(pattern string) error {
	if _, err := path.Match(pattern, ""); err != nil {
		return fmt.Errorf("invalid glob %q: %w", pattern, err)
	}
	return nil
}

// validateAWS checks each AWS account entry. An account may omit profile to
// use the SDK default chain (environment, SSO...), but only one account can do
// so unless it assumes a role: the chain identifies a single account.
func (c *Config) validateAWS() error {
	for i, acc := range c.Clouds.AWS {
		if err := acc.validateKeys(i); err != nil {
			return err
		}
		if acc.Profile == "" && acc.RoleARN == "" && len(c.Clouds.AWS) > 1 {
			return fmt.Errorf("aws[%d]: has no profile or role_arn; with several accounts each one needs its own "+
				"(if this entry only holds regions, nest them under the previous account)", i)
		}
		if err := acc.validateRole(i); err != nil {
			return err
		}

		seen := make(map[string]bool, len(acc.Regions))
		for _, region := range acc.Regions {
			if !awsRegionPattern.MatchString(region) {
				return fmt.Errorf("aws[%d]: invalid region %q", i, region)
			}
			if seen[region] {
				return fmt.Errorf("aws[%d]: region %q listed twice", i, region)
			}
			seen[region] = true
		}
	}
	return nil
}
