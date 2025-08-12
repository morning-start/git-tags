package main

import (
	"fmt"
	"os"
)

func init() {
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(patchCmd)
	rootCmd.AddCommand(minorCmd)
	rootCmd.AddCommand(majorCmd)
	pushCmd.Flags().StringP("branch", "b", "origin", "Specify the branch to push tags to")
	rootCmd.AddCommand(pushCmd)
	delCmd.Flags().StringP("branch", "b", "origin", "Specify the remote branch to delete tags")
	rootCmd.AddCommand(delCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
