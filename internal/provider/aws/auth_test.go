package aws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	ststypes "github.com/aws/aws-sdk-go-v2/service/sts/types"

	cfgpkg "github.com/unicrons/tagctl/internal/config"
)

type mockSTSClient struct {
	input *sts.AssumeRoleInput
}

func (m *mockSTSClient) AssumeRole(ctx context.Context, params *sts.AssumeRoleInput, optFns ...func(*sts.Options)) (*sts.AssumeRoleOutput, error) {
	m.input = params
	return &sts.AssumeRoleOutput{Credentials: &ststypes.Credentials{
		AccessKeyId: aws.String("ASIA"), SecretAccessKey: aws.String("s"), SessionToken: aws.String("t"),
		Expiration: aws.Time(time.Now().Add(time.Hour)),
	}}, nil
}

func TestAssumeRole_Defaults(t *testing.T) {
	mock := &mockSTSClient{}
	cfg := assumeRoleWith(aws.Config{}, mock, cfgpkg.AWSAccount{RoleARN: "arn:aws:iam::111111111111:role/Audit"})

	creds, err := cfg.Credentials.Retrieve(context.Background())
	if err != nil || creds.AccessKeyID != "ASIA" {
		t.Fatalf("creds = %+v, err = %v", creds, err)
	}
	in := mock.input
	if aws.ToString(in.RoleArn) != "arn:aws:iam::111111111111:role/Audit" || aws.ToString(in.RoleSessionName) != defaultRoleSessionName {
		t.Errorf("input = %+v", in)
	}
	if aws.ToInt32(in.DurationSeconds) != 3600 {
		t.Errorf("DurationSeconds = %d, want the one-hour default", aws.ToInt32(in.DurationSeconds))
	}
	if in.ExternalId != nil || in.SerialNumber != nil {
		t.Errorf("unset options must not be sent, got %+v", in)
	}
}

func TestAssumeRole_Options(t *testing.T) {
	mock := &mockSTSClient{}
	cfg := assumeRoleWith(aws.Config{}, mock, cfgpkg.AWSAccount{
		RoleARN: "arn:aws:iam::111111111111:role/Audit", ExternalID: "ext", SessionDuration: 1800, RoleSessionName: "audit",
	})
	if _, err := cfg.Credentials.Retrieve(context.Background()); err != nil {
		t.Fatal(err)
	}
	in := mock.input
	if aws.ToString(in.ExternalId) != "ext" || aws.ToInt32(in.DurationSeconds) != 1800 || aws.ToString(in.RoleSessionName) != "audit" {
		t.Errorf("input = %+v", in)
	}
}

func TestReadMFAToken(t *testing.T) {
	errNoTTY := errors.New("inappropriate ioctl for device")
	cases := []struct {
		name         string
		stdin        string
		isTerminal   bool
		password     string
		passwordErr  error
		want         string
		wantPrompt   string
		wantErr      error
		wantPassword bool
	}{
		{name: "terminal reads without echo", stdin: "leaked\n", isTerminal: true, password: " 123456 ", want: "123456", wantPrompt: "MFA token code: \n", wantPassword: true},
		{name: "terminal error is wrapped", isTerminal: true, passwordErr: errNoTTY, wantPrompt: "MFA token code: \n", wantErr: errNoTTY, wantPassword: true},
		{name: "pipe reads one line", stdin: "654321\nextra\n", want: "654321", wantPrompt: "MFA token code: "},
		{name: "pipe without a line fails", stdin: "", wantPrompt: "MFA token code: ", wantErr: io.EOF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var prompt bytes.Buffer
			passwordRead := false
			readPassword := func() ([]byte, error) {
				passwordRead = true
				return []byte(tc.password), tc.passwordErr
			}

			got, err := readMFAToken(&prompt, strings.NewReader(tc.stdin), tc.isTerminal, readPassword)

			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("token = %q, want %q", got, tc.want)
			}
			if prompt.String() != tc.wantPrompt {
				t.Errorf("prompt = %q, want %q", prompt.String(), tc.wantPrompt)
			}
			if passwordRead != tc.wantPassword {
				t.Errorf("no-echo read used = %v, want %v", passwordRead, tc.wantPassword)
			}
		})
	}
}

func TestInitialRegion(t *testing.T) {
	cases := []struct {
		resolved   string
		configured []string
		want       string
	}{
		{"eu-west-1", []string{"us-west-2"}, "eu-west-1"},
		{"", []string{"us-gov-west-1", "us-gov-east-1"}, "us-gov-west-1"},
		{"", nil, defaultRegion},
	}
	for _, tc := range cases {
		if got := initialRegion(tc.resolved, tc.configured); got != tc.want {
			t.Errorf("initialRegion(%q, %v) = %q, want %q", tc.resolved, tc.configured, got, tc.want)
		}
	}
}
