package prow

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	_ "github.com/mattn/go-sqlite3"
)

func newDBCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "db",
		Short: "Prow database functions.",
	}

	command.AddCommand(newCreateCommand())

	return command
}

var storagePattern = regexp.MustCompile(`.*/(origin-ci-test/.*)`)

type createOptions struct {
	BaseURL        string
	StorageBaseURL string
	From           time.Duration
	Jobs           []string
	OutputFile     string
	DryRun         bool
}

func newCreateCommand() *cobra.Command {
	var options createOptions

	var command = &cobra.Command{
		Use:   "create",
		Short: "Creates or updates a sqlite database with CI build history.",
		Run: func(cmd *cobra.Command, args []string) {
			err := create(options)
			if err != nil {
				panic(err)
			}
		},
	}

	command.Flags().StringVarP(&options.BaseURL, "base-url", "", "https://prow.ci.openshift.org", "")
	command.Flags().StringVarP(&options.StorageBaseURL, "storage-base-url", "", "https://gcsweb-ci.apps.ci.l2s4.p1.openshiftapps.com/gcs", "GCS storage base")
	command.Flags().DurationVarP(&options.From, "from", "", 24*time.Hour, "how far back to find builds")
	command.Flags().StringArrayVarP(&options.Jobs, "job", "", []string{"release-openshift-ocp-installer-e2e-aws-4.6"}, "jobs to find")
	command.Flags().StringVarP(&options.OutputFile, "output-file", "f", path.Join(os.Getenv("HOME"), ".dowser.db"), "output database file location")
	command.Flags().BoolVarP(&options.DryRun, "dry-run", "", false, "output data and exit without writing")

	return command
}

type build struct {
	Build
	Job string
	URL string
}

func create(options createOptions) error {
	buildC := make(chan []build)
	for _, job := range options.Jobs {
		go func(job string) {
			builds, err := findBuilds(options.From, options.BaseURL, options.StorageBaseURL, job)
			if err != nil {
				log.Printf("couldn't find builds for job %s: %s", job, err)
				buildC <- []build{}
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

func findBuilds(from time.Duration, baseURL string, storageBaseURL string, job string) ([]build, error) {
	var builds []build

	jobURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}

	var prefix string
	switch {
	case strings.HasPrefix(job, "pull-"):
		prefix = "job-history/gs/origin-ci-test/pr-logs/directory"
	case strings.HasPrefix(job, "periodic-"):
		prefix = "job-history/gs/origin-ci-test/logs"
	}
	jobURL.Path = path.Join(jobURL.Path, prefix, job)
	prowBuilds, err := GetJobHistory(from, jobURL.String())
	if err != nil {
		return nil, err
	}
	log.Printf("found %d prow builds for job %s", len(prowBuilds), job)

	prowBuildC := make(chan Build, len(prowBuilds))
	for i := range prowBuilds {
		prowBuildC <- prowBuilds[i]
	}

	buildC := make(chan *build)
	done := make(chan bool)
	for i := 0; i < 5; i++ {
		go func() {
			for {
				select {
				case <-done:
					return
				case prowBuild := <-prowBuildC:
					buildURL, err := url.Parse(baseURL)
					if err != nil {
						log.Printf("invalid prow build url %q: %s", baseURL, err)
						buildC <- nil
						break
					}
					buildURL.Path = path.Join(buildURL.Path, prowBuild.SpyglassLink)
					build := build{
						Build: prowBuild,
						Job:   job,
						URL:   buildURL.String(),
					}
					buildC <- &build
				}
			}
		}()
	}
	for range prowBuilds {
		if build := <-buildC; build != nil {
			builds = append(builds, *build)
		}
	}
	close(done)
	return builds, nil
}
