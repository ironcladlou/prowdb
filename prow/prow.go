package prow

import (
	"context"
	"log"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/ironcladlou/prowdb/prow/internal"
	prowio "k8s.io/test-infra/prow/io"
)

const (
	DefaultBaseURL = "https://prow.ci.openshift.org"
)

type Build struct {
	internal.BuildData

	Job string
	URL string
}

func GetJobHistoryByJobName(ctx context.Context, baseURL string, from time.Duration, jobs ...string) ([]Build, error) {
	var builds []Build
	for _, job := range jobs {
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
		log.Println("fetching job history for", jobURL.String())
		prowBuilds, err := GetJobHistoryByJobURL(ctx, baseURL, from, jobURL.String())
		if err != nil {
			return nil, err
		}
		log.Printf("found %d prow builds for job %s", len(prowBuilds), job)
		builds = append(builds, prowBuilds...)
	}
	return builds, nil
}

func GetJobHistoryByJobURL(ctx context.Context, baseURL string, from time.Duration, jobURL string) ([]Build, error) {
	var builds []Build

	u, err := url.Parse(jobURL)
	if err != nil {
		return nil, err
	}

	opener, err := prowio.NewOpener(ctx, "", "")
	if err != nil {
		return nil, err
	}

	cutoff := time.Now().Add(-from).UTC()
	nextURL := u
	for done := false; !done; {
		fetchStarted := time.Now()
		hist, err := internal.GetJobHistory(ctx, nextURL, opener)
		log.Printf("fetched job history from %s in %v", nextURL, time.Since(fetchStarted)/time.Second)
		if err != nil {
			return builds, err
		}
		for _, build := range hist.Builds {
			if build.Started.Before(cutoff) {
				done = true
				break
			}
			buildURL, _ := url.Parse(baseURL)
			buildURL.Path = path.Join(buildURL.Path, build.SpyglassLink)
			builds = append(builds, Build{
				BuildData: build,
				Job:       build.ProwJob.Spec.Job,
				URL:       buildURL.String(),
			})
		}
		olderURL, err := url.Parse(hist.OlderLink)
		if err != nil {
			return nil, err
		}
		nextURL = olderURL
	}

	return builds, nil
}
