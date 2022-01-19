package main

import (
	"github.com/ironcladlou/prowdb/hive"
	"github.com/spf13/cobra"

	"github.com/ironcladlou/prowdb/prow"
)

func main() {
	var cmd = &cobra.Command{Use: "prowdb"}

	cmd.AddCommand(prow.NewCommand())
	cmd.AddCommand(hive.NewCommand())

	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
