package main

import (
	"fmt"
	"os"
)

func init() {
	pushCmd.Flags().StringP("branch", "b", "origin", "Specify the branch to push tags to")
	delCmd.Flags().StringP("branch", "b", "origin", "Specify the remote branch to delete tags")
	rootCmd.AddCommand(lsCmd, patchCmd, minorCmd, majorCmd, pushCmd, delCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
