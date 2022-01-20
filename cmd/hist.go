package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ironcladlou/prowdb/prow"
	"github.com/spf13/cobra"
)

func newHistCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "hist",
		Short: "Prow history tool",
	}

	command.AddCommand(newHistShowCommand())

	return command
}

type histShowOptions struct {
	BaseURL string
	From    time.Duration
	Jobs    []string
}

func newHistShowCommand() *cobra.Command {
	var options histShowOptions

	var command = &cobra.Command{
		Use:   "show",
		Short: "Shows job history in a machine consumable format.",
		Run: func(cmd *cobra.Command, args []string) {
			err := renderHistory(context.TODO(), options)
			if err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&options.BaseURL, "base-url", "", prow.DefaultBaseURL, "")
	command.Flags().DurationVarP(&options.From, "from", "", 24*time.Hour, "how far back to find builds")
	command.Flags().StringArrayVarP(&options.Jobs, "job", "", []string{"pull-ci-openshift-hypershift-main-e2e-aws"}, "jobs to find")

	return command
}

func renderHistory(ctx context.Context, opts histShowOptions) error {
	builds, err := prow.GetJobHistoryByJobName(ctx, opts.BaseURL, opts.From, opts.Jobs...)
	if err != nil {
		return err
	}
	out, err := json.MarshalIndent(builds, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(out))
	return nil
}
