package types

import (
	"testing"
)

func TestResource_HasTag(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		tag      string
		want     bool
	}{
		{
			name: "tag exists",
			resource: Resource{
				Tags: map[string]string{"environment": "prod"},
			},
			tag:  "environment",
			want: true,
		},
		{
			name: "tag does not exist",
			resource: Resource{
				Tags: map[string]string{"environment": "prod"},
			},
			tag:  "owner",
			want: false,
		},
		{
			name: "empty tags",
			resource: Resource{
				Tags: map[string]string{},
			},
			tag:  "environment",
			want: false,
		},
		{
			name: "nil tags",
			resource: Resource{
				Tags: nil,
			},
			tag:  "environment",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resource.HasTag(tt.tag); got != tt.want {
				t.Errorf("HasTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResource_GetTag(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		tag      string
		want     string
	}{
		{
			name: "tag exists",
			resource: Resource{
				Tags: map[string]string{"environment": "prod"},
			},
			tag:  "environment",
			want: "prod",
		},
		{
			name: "tag does not exist",
			resource: Resource{
				Tags: map[string]string{"environment": "prod"},
			},
			tag:  "owner",
			want: "",
		},
		{
			name: "empty value",
			resource: Resource{
				Tags: map[string]string{"environment": ""},
			},
			tag:  "environment",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resource.GetTag(tt.tag); got != tt.want {
				t.Errorf("GetTag() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResource_DisplayName(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		want     string
	}{
		{
			name: "has name",
			resource: Resource{
				ID:   "i-0abc123",
				Name: "web-server-1",
			},
			want: "web-server-1",
		},
		{
			name: "no name uses ID",
			resource: Resource{
				ID:   "i-0abc123",
				Name: "",
			},
			want: "i-0abc123",
		},
		{
			name: "both empty",
			resource: Resource{
				ID:   "",
				Name: "",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resource.DisplayName(); got != tt.want {
				t.Errorf("DisplayName() = %v, want %v", got, tt.want)
			}
		})
	}
}

// AWS repeats names such as log groups across regions, so the identity must
// tell two resources with the same ID apart; a Terraform address has no
// account or region yet and must not carry empty segments.
func TestResource_Identity(t *testing.T) {
	tests := []struct {
		name     string
		resource Resource
		want     string
	}{
		{
			name:     "ARN wins when present",
			resource: Resource{ARN: "arn:aws:ec2:us-east-1:111:instance/i-1", ID: "i-1", Provider: "aws"},
			want:     "arn:aws:ec2:us-east-1:111:instance/i-1",
		},
		{
			name:     "live resource without ARN",
			resource: Resource{ID: "i-1", Provider: "aws", Account: "111", Region: "us-east-1"},
			want:     "aws/111/us-east-1/i-1",
		},
		{
			name:     "terraform address has no account or region",
			resource: Resource{ID: "aws_s3_bucket.logs", Provider: "aws"},
			want:     "aws/aws_s3_bucket.logs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.resource.Identity(); got != tt.want {
				t.Errorf("Identity() = %q, want %q", got, tt.want)
			}
		})
	}

	a := Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "eu-west-1"}
	b := Resource{ID: "/aws/lambda/fn", Provider: "aws", Account: "111", Region: "us-east-1"}
	if a.Identity() == b.Identity() {
		t.Errorf("same ID in two regions share identity %q", a.Identity())
	}
}
