package cmd

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path"
	"time"

	"github.com/ironcladlou/prowdb/prow"
	"github.com/spf13/cobra"

	_ "github.com/mattn/go-sqlite3"
)

func newDBCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "db",
		Short: "Prow database functions.",
	}

	command.AddCommand(newCreateDBCommand())

	return command
}

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

	command.Flags().StringVarP(&options.BaseURL, "base-url", "", "https://prow.ci.openshift.org", "")
	command.Flags().DurationVarP(&options.From, "from", "", 24*time.Hour, "how far back to find builds")
	command.Flags().StringArrayVarP(&options.Jobs, "job", "", []string{"release-openshift-ocp-installer-e2e-aws-4.6"}, "jobs to find")
	command.Flags().StringVarP(&options.OutputFile, "output-file", "f", path.Join(os.Getenv("HOME"), ".dowser.db"), "output database file location")
	command.Flags().BoolVarP(&options.DryRun, "dry-run", "", false, "output data and exit without writing")

	return command
}

func create(ctx context.Context, options createDbOptions) error {
	buildC := make(chan []prow.Build)
	for _, job := range options.Jobs {
		go func(job string) {
			builds, err := prow.GetJobHistoryByJobName(ctx, options.BaseURL, options.From, options.BaseURL, job)
			if err != nil {
				log.Printf("couldn't find builds for job %s: %s", job, err)
				buildC <- []prow.Build{}
			} else {
				buildC <- builds
			}
		}(job)
	}

	if options.DryRun {
		fmt.Println("waiting")
		for range options.Jobs {
			for _, build := range <-buildC {
				fmt.Printf("%#v\n", build)
			}
		}
		return nil
	}

	db, err := sql.Open("sqlite3", options.OutputFile)
	if err != nil {
		return err
	}
	defer db.Close()

	sqlStmt := `
create table if not exists jobs (
  id text not null primary key,
	name text,
	result text,
	url text,
	started text,
	duration numeric,
	prowname text,
	prowjob text
);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	stmt, err := tx.Prepare(`
insert or replace into jobs(id, name, result, url, started, duration, prowname, prowjob)
values(?, ?, ?, ?, ?, ?, ?, ?)
`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for range options.Jobs {
		for _, build := range <-buildC {
			prowJobJSON, err := json.MarshalIndent(build.ProwJob, "", "  ")
			if err != nil {
				log.Printf("error marshalling prowjob json: %v", err)
			}
			_, err = stmt.Exec(build.ID, build.Job, build.Result, build.URL, build.Started.UTC().Format(time.RFC3339), build.Duration, build.ProwJob.Name, string(prowJobJSON))
			if err != nil {
				log.Printf("error inserting:\nbuild: %#v\nerror: %v\n", build, err)
			}
		}
	}
	err = tx.Commit()
	if err != nil {
		return err
	}

	log.Printf("wrote build database to %s", options.OutputFile)
	return nil
}
