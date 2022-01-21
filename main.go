package main

import (
	"github.com/ironcladlou/prowdb/cmd/db"
	"github.com/ironcladlou/prowdb/cmd/hist"
	"github.com/spf13/cobra"
)

func main() {
	var root = &cobra.Command{Use: "prowdb"}

	root.AddCommand(db.NewCommand())
	root.AddCommand(hist.NewCommand())

	if err := root.Execute(); err != nil {
		panic(err)
	}
}
