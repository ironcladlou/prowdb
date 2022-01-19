package hive

import "github.com/spf13/cobra"

func NewCommand() *cobra.Command {
	var command = &cobra.Command{
		Use:   "hive",
		Short: "Hive functions.",
	}

	command.AddCommand(newDBCommand())

	return command
}
