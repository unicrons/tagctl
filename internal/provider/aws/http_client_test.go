package aws

import (
	"testing"

	awshttp "github.com/aws/aws-sdk-go-v2/aws/transport/http"
)

func TestScanHTTPClient_LeavesCredentialClientUntouched(t *testing.T) {
	base := awshttp.NewBuildableClient()
	scan, ok := scanHTTPClient(base).(*awshttp.BuildableClient)
	if !ok {
		t.Fatal("scan client is not buildable")
	}
	if got := scan.GetDialer().Timeout; got != dialTimeout {
		t.Errorf("scan dial timeout = %s, want %s", got, dialTimeout)
	}
	if got := base.GetDialer().Timeout; got == dialTimeout {
		t.Errorf("credential client dial timeout was lowered to %s", got)
	}
}
