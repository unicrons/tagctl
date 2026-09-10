package aws

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/globalaccelerator"
	gatypes "github.com/aws/aws-sdk-go-v2/service/globalaccelerator/types"
	"github.com/aws/aws-sdk-go-v2/service/shield"
	shieldtypes "github.com/aws/aws-sdk-go-v2/service/shield/types"
	"github.com/aws/aws-sdk-go-v2/service/waf"
	waftypes "github.com/aws/aws-sdk-go-v2/service/waf/types"
)

type mockShieldClient struct {
	protections []shieldtypes.Protection
	err         error
}

func (m *mockShieldClient) ListProtections(ctx context.Context, params *shield.ListProtectionsInput, optFns ...func(*shield.Options)) (*shield.ListProtectionsOutput, error) {
	return &shield.ListProtectionsOutput{Protections: m.protections}, m.err
}

type mockWAFGlobalClient struct {
	acls []waftypes.WebACLSummary
	err  error
}

func (m *mockWAFGlobalClient) ListWebACLs(ctx context.Context, params *waf.ListWebACLsInput, optFns ...func(*waf.Options)) (*waf.ListWebACLsOutput, error) {
	return &waf.ListWebACLsOutput{WebACLs: m.acls}, m.err
}

type mockGlobalAcceleratorClient struct {
	accelerators []gatypes.Accelerator
	tags         map[string][]gatypes.Tag
	err          error
	tagsErr      error
	tagged       *globalaccelerator.TagResourceInput
}

func (m *mockGlobalAcceleratorClient) ListAccelerators(ctx context.Context, params *globalaccelerator.ListAcceleratorsInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListAcceleratorsOutput, error) {
	return &globalaccelerator.ListAcceleratorsOutput{Accelerators: m.accelerators}, m.err
}

func (m *mockGlobalAcceleratorClient) ListTagsForResource(ctx context.Context, params *globalaccelerator.ListTagsForResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.ListTagsForResourceOutput, error) {
	if m.tagsErr != nil {
		return nil, m.tagsErr
	}
	return &globalaccelerator.ListTagsForResourceOutput{Tags: m.tags[aws.ToString(params.ResourceArn)]}, nil
}

func (m *mockGlobalAcceleratorClient) TagResource(ctx context.Context, params *globalaccelerator.TagResourceInput, optFns ...func(*globalaccelerator.Options)) (*globalaccelerator.TagResourceOutput, error) {
	m.tagged = params
	return &globalaccelerator.TagResourceOutput{}, m.err
}

func TestListProtections(t *testing.T) {
	p := bulkProvider(map[string]map[string]string{"arn:shield:p1": {"owner": "x"}})
	resources, err := p.listProtectionsFrom(context.Background(), &mockShieldClient{protections: []shieldtypes.Protection{{Id: aws.String("p1"), Name: aws.String("alb"), ProtectionArn: aws.String("arn:shield:p1")}}})
	if err != nil || len(resources) != 1 || resources[0].Region != regionGlobal || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}

	unsubscribed := &mockShieldClient{err: apiError{"ResourceNotFoundException"}}
	if resources, err := p.listProtectionsFrom(context.Background(), unsubscribed); err != nil || len(resources) != 0 {
		t.Errorf("no subscription must be skipped silently, got %+v, %v", resources, err)
	}
	if _, err := p.listProtectionsFrom(context.Background(), &mockShieldClient{err: errors.New("boom")}); err == nil {
		t.Error("expected error")
	}
}

func TestListGlobalWebACLs(t *testing.T) {
	arn := "arn:aws:waf::123456789012:webacl/w1"
	p := bulkProvider(map[string]map[string]string{arn: {"owner": "x"}})
	resources, err := p.listGlobalWebACLsFrom(context.Background(), &mockWAFGlobalClient{acls: []waftypes.WebACLSummary{{WebACLId: aws.String("w1"), Name: aws.String("edge")}}})
	if err != nil || len(resources) != 1 || resources[0].ARN != arn || resources[0].Region != regionGlobal || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}
	if _, err := p.listGlobalWebACLsFrom(context.Background(), &mockWAFGlobalClient{err: errors.New("boom")}); err == nil {
		t.Error("expected error")
	}
	if resources, err := testProvider().listGlobalWebACLsFrom(context.Background(), &mockWAFGlobalClient{acls: []waftypes.WebACLSummary{{WebACLId: aws.String("x")}}}); err != nil || len(resources) != 0 {
		t.Errorf("without bulk tags want nothing, got %+v, %v", resources, err)
	}
}

func TestListAccelerators(t *testing.T) {
	arn := "arn:aws:globalaccelerator::123456789012:accelerator/abc"
	mock := &mockGlobalAcceleratorClient{
		accelerators: []gatypes.Accelerator{{AcceleratorArn: aws.String(arn), Name: aws.String("edge")}},
		tags:         map[string][]gatypes.Tag{arn: {{Key: aws.String("owner"), Value: aws.String("x")}}},
	}
	resources, err := testProvider().listAcceleratorsFrom(context.Background(), mock)
	if err != nil || len(resources) != 1 || resources[0].ID != "abc" || resources[0].Region != regionGlobal || resources[0].Tags["owner"] != "x" {
		t.Errorf("resources = %+v, err = %v", resources, err)
	}

	mock.tagsErr = errors.New("denied")
	if resources, err := testProvider().listAcceleratorsFrom(context.Background(), mock); err != nil || len(resources) != 0 {
		t.Errorf("unreadable tags must skip the accelerator, got %+v, %v", resources, err)
	}
	if _, err := testProvider().listAcceleratorsFrom(context.Background(), &mockGlobalAcceleratorClient{err: errors.New("boom")}); err == nil {
		t.Error("expected error")
	}
}

func TestApplyGlobalAcceleratorTags(t *testing.T) {
	mock := &mockGlobalAcceleratorClient{}
	if err := applyGlobalAcceleratorTagsWith(context.Background(), mock, "arn:ga", map[string]string{"owner": "x"}); err != nil || mock.tagged == nil || len(mock.tagged.Tags) != 1 {
		t.Errorf("err = %v, input = %+v", err, mock.tagged)
	}
	if err := applyGlobalAcceleratorTagsWith(context.Background(), &mockGlobalAcceleratorClient{err: errors.New("boom")}, "arn:ga", nil); err == nil {
		t.Error("expected error")
	}
}
