package db

import (
	"github.com/spf13/cobra"
)

func NewCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "db",
		Short: "Prow database functions.",
	}

	command.AddCommand(newCreateDBCommand())

	return command
}
