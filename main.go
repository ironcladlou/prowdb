package main

import (
	"github.com/spf13/cobra"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"github.com/ironcladlou/ez-thanos-operator/db"
	"github.com/ironcladlou/ez-thanos-operator/operator"
)

func main() {
	var cmd = &cobra.Command{Use: "ez-thanos-operator"}
	cmd.AddCommand(operator.NewStartCommand())
	cmd.AddCommand(db.NewDBCommand())

	if err := cmd.Execute(); err != nil {
		panic(err)
	}
}
