package aws

import (
	"github.com/aws/aws-sdk-go-v2/aws/arn"

	"github.com/unicrons/tagctl/internal/types"
)

// noSourceVolume is what EC2 reports for a snapshot that has no source volume.
const noSourceVolume = "vol-ffffffff"

// withEC2Parent records the EC2 resource r hangs from under relation. The
// parent shares r's ARN up to the resource part, so nothing is recorded
// without r.ARN or a parent id.
func withEC2Parent(r types.Resource, relation, arnType, parentID string) types.Resource {
	if parentID == "" || parentID == noSourceVolume {
		return r
	}
	parent, err := arn.Parse(r.ARN)
	if err != nil {
		return r
	}
	parent.Resource = arnType + "/" + parentID
	if r.Parents == nil {
		r.Parents = make(map[string]string, 1)
	}
	r.Parents[relation] = parent.String()
	return r
}
