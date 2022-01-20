package cmd

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "prow",
		Short: "Prow functions.",
	}

	command.AddCommand(newDBCommand())
	command.AddCommand(newHistCommand())

	return command
}
