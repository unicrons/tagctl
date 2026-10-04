package aws

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/costexplorer"
	cetypes "github.com/aws/aws-sdk-go-v2/service/costexplorer/types"

	"github.com/unicrons/tagctl/internal/log"
	"github.com/unicrons/tagctl/internal/provider"
	"github.com/unicrons/tagctl/internal/types"
)

// costExplorerAPI is the subset of the Cost Explorer API used here.
type costExplorerAPI interface {
	GetCostAndUsage(ctx context.Context, params *costexplorer.GetCostAndUsageInput, optFns ...func(*costexplorer.Options)) (*costexplorer.GetCostAndUsageOutput, error)
}

// unattributedValue is the group key Cost Explorer returns for spend on
// resources that do not carry the tag being grouped by.
const unattributedKey = ""

// CostReport returns how much of the account's spend each tag accounts for,
// over the given period.
//
// Cost Explorer groups spend by tag key, returning one group per value plus a
// group with an empty value for everything that does not carry the tag. That
// empty group is the number that matters: it is the spend nobody can be billed
// for.
//
// A non-empty trend also splits each tag's spend into periods of that
// granularity, from the same request.
func (p *Provider) CostReport(ctx context.Context, tags []string, start, end time.Time, trend types.CostGranularity) (*types.CostReport, error) {
	return p.costReportFrom(ctx, regionalClient(p, p.globalRegion(), costexplorer.NewFromConfig), tags, start, end, trend)
}

// costReportFrom builds the report using the given client.
func (p *Provider) costReportFrom(ctx context.Context, client costExplorerAPI, tags []string, start, end time.Time, trend types.CostGranularity) (*types.CostReport, error) {
	if len(tags) == 0 {
		return nil, provider.NewProviderError(providerName, "cost_report", "",
			fmt.Errorf("no tags to report on: define required tags in your policy or pass --tag"))
	}

	report := &types.CostReport{
		Start: start,
		End:   end,
		Tags:  make(map[string]*types.TagCost, len(tags)),
	}

	for _, tag := range tags {
		tagCost, currency, err := p.costForTag(ctx, client, tag, start, end, trend)
		if err != nil {
			return nil, err
		}

		report.Tags[tag] = tagCost
		if report.Currency == "" {
			report.Currency = currency
		}

		// Every tag is measured over the same spend, so the total is taken
		// once rather than summed across tags.
		if report.Total == 0 {
			report.Total = tagCost.Total()
		}
	}

	return report, nil
}

// costForTag asks Cost Explorer for spend grouped by one tag key. A trend is
// bucketed from the daily results of that same query: Cost Explorer bills per
// request and has no weekly granularity.
func (p *Provider) costForTag(ctx context.Context, client costExplorerAPI, tag string, start, end time.Time, trend types.CostGranularity) (*types.TagCost, string, error) {
	log.Debug("AWS CostExplorer: Getting cost grouped by tag %s", tag)

	tagCost := &types.TagCost{Tag: tag}
	currency := ""

	granularity := cetypes.GranularityMonthly
	var periods []types.CostPeriod
	if trend != "" {
		granularity = cetypes.GranularityDaily
		periods = types.CostPeriods(start, end, trend)
	}

	input := &costexplorer.GetCostAndUsageInput{
		TimePeriod: &cetypes.DateInterval{
			Start: aws.String(start.Format(costDateLayout)),
			End:   aws.String(end.Format(costDateLayout)),
		},
		Granularity: granularity,
		Metrics:     []string{"UnblendedCost"},
		GroupBy: []cetypes.GroupDefinition{
			{Type: cetypes.GroupDefinitionTypeTag, Key: aws.String(tag)},
		},
	}

	byValue := make(map[string]float64)

	for {
		output, err := client.GetCostAndUsage(ctx, input)
		if err != nil {
			return nil, "", provider.NewProviderError(providerName, "get_cost_and_usage", tag, err)
		}

		for _, result := range output.ResultsByTime {
			period := periodOf(periods, result.TimePeriod)
			if trend != "" && period == nil {
				log.Debug("AWS CostExplorer: Result for %s has no day inside the window, left out of the trend", tag)
			}

			for _, group := range result.Groups {
				amount, unit, parseErr := amountOf(group.Metrics)
				if parseErr != nil {
					log.Debug("AWS CostExplorer: Skipping unparsable amount for %s: %v", tag, parseErr)
					continue
				}
				if unit != "" && currency == "" {
					currency = unit
				}

				value := tagValueOf(group.Keys, tag)
				byValue[value] += amount

				if value == unattributedKey {
					tagCost.Unattributed += amount
				} else {
					tagCost.Attributed += amount
				}

				if period == nil {
					continue
				}
				if value == unattributedKey {
					period.Unattributed += amount
				} else {
					period.Attributed += amount
				}
			}
		}

		if output.NextPageToken == nil || *output.NextPageToken == "" {
			break
		}
		input.NextPageToken = output.NextPageToken
	}

	tagCost.Values = sortedValues(byValue)
	if trend != "" {
		tagCost.Trend = types.NewCostTrend(trend, periods)
	}

	log.Debug("AWS CostExplorer: Tag %s covers %.2f of %.2f (%.1f%%)",
		tag, tagCost.Attributed, tagCost.Total(), tagCost.CoveragePct())

	return tagCost, currency, nil
}

// costDateLayout is how Cost Explorer writes a day.
const costDateLayout = "2006-01-02"

// periodOf returns the period a Cost Explorer result starts in, or nil.
func periodOf(periods []types.CostPeriod, interval *cetypes.DateInterval) *types.CostPeriod {
	if len(periods) == 0 || interval == nil {
		return nil
	}

	day, err := time.Parse(costDateLayout, aws.ToString(interval.Start))
	if err != nil {
		return nil
	}

	for i := range periods {
		if periods[i].Contains(day) {
			return &periods[i]
		}
	}
	return nil
}

// amountOf pulls the unblended cost and its unit out of a group's metrics.
func amountOf(metrics map[string]cetypes.MetricValue) (float64, string, error) {
	metric, ok := metrics["UnblendedCost"]
	if !ok {
		return 0, "", fmt.Errorf("no UnblendedCost metric in group")
	}

	amount, err := strconv.ParseFloat(aws.ToString(metric.Amount), 64)
	if err != nil {
		return 0, "", fmt.Errorf("unparsable amount %q: %w", aws.ToString(metric.Amount), err)
	}

	return amount, aws.ToString(metric.Unit), nil
}

// tagValueOf extracts the value from a Cost Explorer group key, which comes
// back as "tagKey$tagValue" with an empty value for untagged spend.
func tagValueOf(keys []string, tag string) string {
	if len(keys) == 0 {
		return unattributedKey
	}

	key := keys[0]
	prefix := tag + "$"

	if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
		return key[len(prefix):]
	}

	return key
}

// sortedValues turns the per-value totals into a list, most expensive first.
func sortedValues(byValue map[string]float64) []types.ValueCost {
	values := make([]types.ValueCost, 0, len(byValue))
	for value, amount := range byValue {
		values = append(values, types.ValueCost{Value: value, Amount: amount})
	}

	sort.Slice(values, func(i, j int) bool {
		if values[i].Amount != values[j].Amount {
			return values[i].Amount > values[j].Amount
		}
		return values[i].Value < values[j].Value
	})

	return values
}
