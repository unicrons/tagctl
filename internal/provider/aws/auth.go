package aws

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"

	cfgpkg "github.com/unicrons/tagctl/internal/config"
	"github.com/unicrons/tagctl/internal/log"
)

// AssumeRole defaults: the session name identifies tagctl in CloudTrail and
// the duration matches STS's own default rather than the SDK's 15 minutes.
const (
	defaultRoleSessionName = "tagctl"
	defaultSessionDuration = time.Hour
)

// assumeRole replaces the credentials of cfg with the role in account,
// refreshed automatically before they expire.
func assumeRole(cfg aws.Config, account cfgpkg.AWSAccount) aws.Config {
	return assumeRoleWith(cfg, sts.NewFromConfig(cfg), account)
}

func assumeRoleWith(cfg aws.Config, client stscreds.AssumeRoleAPIClient, account cfgpkg.AWSAccount) aws.Config {
	log.Info("AWS: Assuming role %s", account.RoleARN)
	provider := stscreds.NewAssumeRoleProvider(client, account.RoleARN, func(o *stscreds.AssumeRoleOptions) {
		o.RoleSessionName = account.RoleSessionName
		if o.RoleSessionName == "" {
			o.RoleSessionName = defaultRoleSessionName
		}
		if account.ExternalID != "" {
			o.ExternalID = aws.String(account.ExternalID)
		}
		o.Duration = defaultSessionDuration
		if account.SessionDuration > 0 {
			o.Duration = time.Duration(account.SessionDuration) * time.Second
		}
		if account.MFASerial != "" {
			o.SerialNumber = aws.String(account.MFASerial)
			o.TokenProvider = mfaTokenFromStdin
		}
	})
	cfg.Credentials = aws.NewCredentialsCache(provider)
	return cfg
}

// mfaTokenFromStdin prompts on stderr so the code never lands in a report
// redirected from stdout.
func mfaTokenFromStdin() (string, error) {
	fmt.Fprint(os.Stderr, "MFA token code: ")
	code, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading MFA token: %w", err)
	}
	return strings.TrimSpace(code), nil
}
