// SPDX-License-Identifier: MIT
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/wippyai/builder/internal/assemble"
)

func color(text, code string) string {
	info, err := os.Stderr.Stat()
	if err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}
func newCommand() *cobra.Command {
	root := &cobra.Command{Use: "wippy-builder", Short: "Assemble native Wippy applications", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(newBuildCommand(false), newBuildCommand(true), newPackCommand(), newSealCommand(), newPackageCommand())
	root.AddCommand(&cobra.Command{Use: "validate MANIFEST", Short: "Validate pinned assembly inputs", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { _, err := assemble.ReadManifest(args[0]); return err }})
	return root
}
func newBuildCommand(toolchain bool) *cobra.Command {
	name, description := "build", "Build a standalone application"
	if toolchain {
		name, description = "toolchain", "Build the selected native Wippy toolchain"
	}
	var output string
	command := &cobra.Command{Use: name + " MANIFEST", Short: description, Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if output == "" {
			return errors.New("--output is required")
		}
		fmt.Fprintln(command.ErrOrStderr(), color(description+"…", "36"))
		return assemble.Build(args[0], output, toolchain)
	}}
	command.Flags().StringVarP(&output, "output", "o", "", "Executable output path")
	return command
}
func newPackCommand() *cobra.Command {
	var toolchain, version string
	command := &cobra.Command{Use: "pack MANIFEST", Short: "Pack a self-contained application and record its checksum", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return assemble.PackRoot(args[0], toolchain, version) }}
	command.Flags().StringVar(&toolchain, "toolchain", "wippy", "Native Wippy toolchain path")
	command.Flags().StringVar(&version, "version", "", "Application version")
	return command
}
func newSealCommand() *cobra.Command {
	var version string
	command := &cobra.Command{Use: "seal MANIFEST", Short: "Record checksums for intentionally regenerated packs", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error { return assemble.Seal(args[0], version) }}
	command.Flags().StringVar(&version, "version", "", "Root application version")
	return command
}
func newPackageCommand() *cobra.Command {
	var output string
	command := &cobra.Command{Use: "package BINARY", Short: "Verify and archive a complete build", Args: cobra.ExactArgs(1), RunE: func(_ *cobra.Command, args []string) error {
		if output == "" {
			return errors.New("--output is required")
		}
		return assemble.Package(args[0], output)
	}}
	command.Flags().StringVarP(&output, "output", "o", "", "Release archive path")
	return command
}
func execute(args []string) error {
	command := newCommand()
	command.SetArgs(args)
	return command.Execute()
}
func main() {
	if err := execute(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, color("wippy-builder: "+err.Error(), "31"))
		os.Exit(1)
	}
}
