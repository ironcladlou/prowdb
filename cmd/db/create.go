package db

import (
	"context"
	_ "embed"
	"encoding/json"
	"log"
	"strings"
	"time"

	"github.com/ironcladlou/prowdb/prow"

	"github.com/spf13/cobra"
	"zombiezen.com/go/sqlite"
	"zombiezen.com/go/sqlite/sqlitex"
)

//go:embed create.sql
var createQuery string

//go:embed update.sql
var updateQuery string

type createDbOptions struct {
	BaseURL    string
	From       time.Duration
	Jobs       []string
	OutputFile string
	DryRun     bool
}

func newCreateDBCommand() *cobra.Command {
	var options createDbOptions

	var command = &cobra.Command{
		Use:   "create",
		Short: "Creates or updates a sqlite database with CI build history.",
		Run: func(cmd *cobra.Command, args []string) {
			err := create(context.TODO(), options)
			if err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&options.BaseURL, "base-url", "", prow.DefaultBaseURL, "")
	command.Flags().DurationVarP(&options.From, "from", "", 24*time.Hour, "how far back to find builds")
	command.Flags().StringArrayVarP(&options.Jobs, "job", "", []string{"pull-ci-openshift-hypershift-main-e2e-aws"}, "jobs to find")
	command.Flags().StringVarP(&options.OutputFile, "output-file", "f", "prow.db", "output database file location")
	command.Flags().BoolVarP(&options.DryRun, "dry-run", "", false, "output data and exit without writing")

	return command
}

func create(ctx context.Context, opts createDbOptions) error {
	conn, err := sqlite.OpenConn(opts.OutputFile, sqlite.OpenReadWrite, sqlite.OpenCreate)
	if err != nil {
		return err
	}
	defer conn.Close()

	// Create the jobs table
	err = sqlitex.ExecuteTransient(conn, createQuery, &sqlitex.ExecOptions{})
	if err != nil {
		return err
	}

	builds, err := prow.GetJobHistoryByJobName(ctx, opts.BaseURL, opts.From, opts.Jobs...)
	if err != nil {
		return err
	}

	log.Printf("found %d builds", len(builds))

	for _, build := range builds {
		prowJson, err := json.MarshalIndent(build.ProwJob, "", "  ")
		if err != nil {
			return err
		}
		err = sqlitex.ExecuteTransient(conn, updateQuery, &sqlitex.ExecOptions{Named: map[string]interface{}{
			"$id":       build.ProwJob.Name,
			"$name":     build.Job,
			"$result":   strings.ToLower(build.Result),
			"$started":  build.Started,
			"$duration": build.Duration,
			"$url":      build.URL,
			"$prowjob":  prowJson,
		}})
		if err != nil {
			return err
		}
	}

	log.Printf("wrote %d records to %s", len(builds), opts.OutputFile)
	return nil
}
