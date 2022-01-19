package prow

import (
	"context"
	"log"
	"net/url"
	"time"

	"github.com/spf13/cobra"
	prowio "k8s.io/test-infra/prow/io"
)

func NewCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "prow",
		Short: "Prow functions.",
	}

	command.AddCommand(newDBCommand())

	return command
}

type Build struct {
	buildData
}

func GetJobHistory(from time.Duration, jobURL string) ([]Build, error) {
	var builds []Build

	u, err := url.Parse(jobURL)
	if err != nil {
		return builds, err
	}

	ctx := context.TODO()

	opener, err := prowio.NewOpener(ctx, "", "")
	if err != nil {
		return builds, err
	}

	cutoff := time.Now().Add(-from).UTC()
	nextURL := u
	for {
		fetchStarted := time.Now()
		hist, err := getJobHistory(ctx, nextURL, opener)
		log.Printf("fetched job history from %s in %v", nextURL, time.Since(fetchStarted)/time.Second)
		if err != nil {
			return builds, err
		}
		for _, build := range hist.Builds {
			if build.Started.Before(cutoff) {
				return builds, nil
			}
			builds = append(builds, Build{buildData: build})
		}
		olderURL, err := url.Parse(hist.OlderLink)
		if err != nil {
			return builds, err
		}
		nextURL = olderURL
	}
}
