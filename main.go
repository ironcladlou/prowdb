package main

import (
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/ironcladlou/dowser/db"
	"github.com/ironcladlou/dowser/operator"
)

func main() {
	var cmd = &cobra.Command{Use: "dowser"}
	cmd.AddCommand(operator.NewStartCommand())
	cmd.AddCommand(db.NewDBCommand())

	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
