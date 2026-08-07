package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := newRootCommand()
	command.SetArgs(args)
	return command.Execute()
}

func newRootCommand() *cobra.Command {
	command := &cobra.Command{
		Use:           "mast-selfhost",
		Short:         "Pixel Telo 自建实时号码查询服务",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	command.AddCommand(newInitCommand(), newServeCommand(), newPairingCommand(), newVersionCommand())
	return command
}

func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出版本信息",
		Args:  cobra.NoArgs,
		Run: func(_ *cobra.Command, _ []string) {
			fmt.Println(versionString())
		},
	}
}
