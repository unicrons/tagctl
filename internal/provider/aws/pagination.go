package aws

import (
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
)

// errRepeatedPageToken means a service sent a page token it had already sent,
// so following it would page forever.
var errRepeatedPageToken = errors.New("service repeated a page token")

// paginate calls fetch with each page token, starting with none, until a page
// comes back without a next token. An empty page does not end the listing.
func paginate(fetch func(token *string) (next *string, err error)) error {
	var token *string
	seen := map[string]bool{}
	for {
		next, err := fetch(token)
		if err != nil {
			return err
		}
		if aws.ToString(next) == "" {
			return nil
		}
		if seen[*next] {
			return errRepeatedPageToken
		}
		seen[*next] = true
		token = next
	}
}
