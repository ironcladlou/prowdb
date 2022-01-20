package main

import (
	"github.com/spf13/cobra"

	"github.com/ironcladlou/prowdb/cmd"
)

func main() {
	var root = &cobra.Command{Use: "prowdb"}

	root.AddCommand(cmd.NewCommand())

	if err := root.Execute(); err != nil {
		panic(err)
	}
}
