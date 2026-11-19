package query

import (
	"context"
	"fmt"
	"slices"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
)

// Groups returns the named log groups plus every log group whose name starts with one of
// the prefixes, each group once.
func (r *Runner) Groups(ctx context.Context, names, prefixes []string) ([]string, error) {
	groups := slices.Clone(names)
	for _, prefix := range prefixes {
		found, err := r.groupsWithPrefix(ctx, prefix)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			return nil, fmt.Errorf("no log group starts with %s", prefix)
		}
		groups = append(groups, found...)
	}
	slices.Sort(groups)
	return slices.Compact(groups), nil
}

func (r *Runner) groupsWithPrefix(ctx context.Context, prefix string) ([]string, error) {
	pages := cloudwatchlogs.NewDescribeLogGroupsPaginator(r.Logs, &cloudwatchlogs.DescribeLogGroupsInput{
		LogGroupNamePrefix: aws.String(prefix),
	})

	var names []string
	for pages.HasMorePages() {
		page, err := pages.NextPage(ctx)
		if err != nil {
			return nil, err
		}
		for _, group := range page.LogGroups {
			names = append(names, aws.ToString(group.LogGroupName))
		}
	}
	return names, nil
}
