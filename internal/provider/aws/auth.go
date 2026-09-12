package aws

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"golang.org/x/term"

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
	fd := int(os.Stdin.Fd())
	return readMFAToken(os.Stderr, os.Stdin, term.IsTerminal(fd), func() ([]byte, error) {
		return term.ReadPassword(fd)
	})
}

// readMFAToken reads the code without echo from a terminal and as a plain
// line from a pipe, where there is nothing to hide and no terminal to configure.
func readMFAToken(prompt io.Writer, in io.Reader, isTerminal bool, readPassword func() ([]byte, error)) (string, error) {
	fmt.Fprint(prompt, "MFA token code: ")
	if isTerminal {
		code, err := readPassword()
		// The typed Enter is not echoed either, so the next line would join the prompt.
		fmt.Fprintln(prompt)
		if err != nil {
			return "", fmt.Errorf("reading MFA token: %w", err)
		}
		return strings.TrimSpace(string(code)), nil
	}
	code, err := bufio.NewReader(in).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("reading MFA token: %w", err)
	}
	return strings.TrimSpace(code), nil
}
