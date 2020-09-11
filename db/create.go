package db

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/ironcladlou/dowser/prow"
)

var storagePattern = regexp.MustCompile(`.*/(origin-ci-test/.*)`)

type createOptions struct {
	BaseURL        string
	StorageBaseURL string
	From           time.Duration
	Jobs           []string
	OutputFile     string
}

func newCreateCommand() *cobra.Command {
	var options createOptions

	var command = &cobra.Command{
		Use:   "create",
		Short: "Creates a CI build history database.",
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
	command.Flags().StringVarP(&options.OutputFile, "output-file", "f", path.Join(os.Getenv("HOME"), ".prow-build-cache.json"), "output database file location")

	return command
}

func create(options createOptions) error {
	var builds []Build

	buildC := make(chan []Build)
	for _, job := range options.Jobs {
		go func(job string) {
			builds, err := findBuilds(options.From, options.BaseURL, options.StorageBaseURL, job)
			if err != nil {
				log.Printf("couldn't find builds for job %s: %s", job, err)
				buildC <- []Build{}
			} else {
				buildC <- builds
			}
		}(job)
	}
	for range options.Jobs {
		builds = append(builds, <-buildC...)
	}

	jsonData, _ := json.MarshalIndent(builds, "", " ")
	err := ioutil.WriteFile(options.OutputFile, jsonData, 0644)
	if err != nil {
		return err
	}
	for _, build := range builds {
		log.Printf("found build, started at %v (%s): %s", build.Started.Format(time.RFC3339), build.Result, build.URL)
	}
	log.Printf("wrote build database to %s", options.OutputFile)
	return nil
}

func findBuilds(from time.Duration, baseURL string, storageBaseURL string, job string) ([]Build, error) {
	var builds []Build

	jobURL, err := url.Parse(baseURL)
	if err != nil {
		return nil, err
	}
	jobURL.Path = path.Join(jobURL.Path, "job-history/gs/origin-ci-test/logs", job)
	prowBuilds, err := prow.GetJobHistory(from, jobURL.String())
	if err != nil {
		return nil, err
	}
	log.Printf("found %d prow builds for job %s", len(prowBuilds), job)

	prowBuildC := make(chan prow.Build, len(prowBuilds))
	for i := range prowBuilds {
		prowBuildC <- prowBuilds[i]
	}

	buildC := make(chan *Build)
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
					build := Build{
						Build: prowBuild,
						Job:   job,
						URL:   buildURL.String(),
					}
					if build.Result != "Pending" {
						metricsTarURL, err := findPrometheusTarURL(build, storageBaseURL)
						if err != nil {
							log.Printf("couldn't find prometheus tar URL for build %s: %s", build.URL, err)
						} else {
							build.PrometheusTarURL = metricsTarURL
						}
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

func findPrometheusTarURL(build Build, storageBaseURL string) (string, error) {
	if len(build.URL) == 0 {
		return "", fmt.Errorf("no build URL found")
	}

	storagePath := storagePattern.FindStringSubmatch(build.URL)[1]
	args := []string{"ls", "gs://" + storagePath + "/**/prometheus.tar"}
	log.Printf("executing command: %v", args)
	cmd := exec.Command("gsutil", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	gsURL, err := url.Parse(strings.ReplaceAll(string(output), "\n", ""))
	if err != nil {
		return "", err
	}
	tarURL, err := url.Parse(storageBaseURL)
	if err != nil {
		return "", err
	}
	tarURL.Path = path.Join(tarURL.Path, gsURL.Host, gsURL.Path)
	return tarURL.String(), nil
}
