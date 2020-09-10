package db

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"sigs.k8s.io/yaml"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	corev1 "k8s.io/api/core/v1"

	"github.com/ironcladlou/ez-thanos-operator/api"
)

var (
	coreScheme = runtime.NewScheme()
	coreCodecs = serializer.NewCodecFactory(coreScheme)
)

func init() {
	if err := corev1.AddToScheme(coreScheme); err != nil {
		panic(err)
	}
}

const (
	outputList    string = "list"
	outputCluster string = "cluster"
)

type selectOptions struct {
	Jobs         []string
	From         time.Duration
	To           time.Duration
	Result       string
	MaxPerDay    int
	OutputFormat string
}

func newSelectCommand() *cobra.Command {
	var options selectOptions

	var dbFile string

	var command = &cobra.Command{
		Use:   "select",
		Short: "Creates a CI build history database.",
		Run: func(cmd *cobra.Command, args []string) {
			builds, err := LoadBuilds(dbFile)
			if err != nil {
				panic(err)
			}
			err = selectBuilds(options, builds)
			if err != nil {
				panic(err)
			}
		},
	}

	command.Flags().DurationVarP(&options.From, "from", "", 24*time.Hour, "start this long ago")
	command.Flags().DurationVarP(&options.From, "to", "", 0, "end this long ago")
	command.Flags().StringVarP(&options.Result, "result", "", "", "result filter")
	command.Flags().StringArrayVarP(&options.Jobs, "job", "", []string{}, "job filter")
	command.Flags().IntVarP(&options.MaxPerDay, "max-per-day", "", 0, "max jobs per day")
	command.Flags().StringVarP(&dbFile, "db-file", "f", path.Join(os.Getenv("HOME"), ".prow-build-cache.json"), "database file location")
	command.Flags().StringVarP(&options.OutputFormat, "output", "o", outputList, "output format: list, metricsCluster=name")

	return command
}

func selectBuilds(options selectOptions, builds []Build) error {
	var filtered []Build
	desiredJobs := sets.NewString(options.Jobs...)

	earliestStarted := time.Now().Add(-options.From)
	latestStarted := time.Now().Add(options.To)
	for _, build := range builds {
		if build.Started.Before(earliestStarted) {
			continue
		}
		if build.Started.After(latestStarted) {
			continue
		}
		if len(options.Result) > 0 && build.Result != options.Result {
			continue
		}
		if len(desiredJobs) > 0 && !desiredJobs.Has(build.Job) {
			continue
		}
		filtered = append(filtered, build)
	}

	byJob := map[string][]Build{}
	for _, build := range filtered {
		if _, exists := byJob[build.Job]; !exists {
			byJob[build.Job] = []Build{}
		}
		byJob[build.Job] = append(byJob[build.Job], build)
	}

	filtered = []Build{}
	for _, builds := range byJob {
		byDay := map[int][]Build{}
		for _, build := range builds {
			day := build.Started.Day()
			if _, exists := byDay[day]; !exists {
				byDay[day] = []Build{}
			}
			if options.MaxPerDay == 0 || len(byDay[day]) < options.MaxPerDay {
				byDay[day] = append(byDay[day], build)
			}
		}
		for _, buildsByDay := range byDay {
			for _, build := range buildsByDay {
				filtered = append(filtered, build)
			}
		}
	}

	switch {
	case options.OutputFormat == outputList:
		out, err := json.MarshalIndent(filtered, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(out))
	case strings.HasPrefix(options.OutputFormat, outputCluster):
		name := strings.Split(options.OutputFormat, "=")[1]
		cluster := &api.MetricsCluster{
			TypeMeta: metav1.TypeMeta{
				Kind:       "MetricsCluster",
				APIVersion: "ez-thanos-operator/v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		for _, build := range filtered {
			cluster.Spec.URLs = append(cluster.Spec.URLs, build.URL)
		}
		data, err := yaml.Marshal(cluster)
		if err != nil {
			return err
		}
		cm := &corev1.ConfigMap{
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Data: map[string]string{
				"cluster.yaml": string(data),
			},
		}
		out := runtime.EncodeOrDie(coreCodecs.LegacyCodec(corev1.SchemeGroupVersion), cm)
		fmt.Println(out)
	}

	return nil
}
