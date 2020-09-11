package db

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/spf13/cobra"

	"github.com/ironcladlou/dowser/prow"
)

type Build struct {
	prow.Build

	Job              string
	URL              string
	PrometheusTarURL string
}

func NewDBCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "db",
		Short: "Prow build database functions.",
	}

	command.AddCommand(newCreateCommand())
	command.AddCommand(newSelectCommand())

	return command
}

func LoadBuilds(file string) ([]Build, error) {
	jsonFile, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer jsonFile.Close()
	data, err := ioutil.ReadAll(jsonFile)

	var builds []Build
	err = json.Unmarshal(data, &builds)
	if err != nil {
		return nil, err
	}
	return builds, nil
}
