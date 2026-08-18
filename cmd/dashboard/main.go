package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/PhilipSchmid/flow-generator-app/internal/config"
	dashboardui "github.com/PhilipSchmid/flow-generator-app/internal/dashboard"
	"github.com/PhilipSchmid/flow-generator-app/internal/version"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/term"
	"github.com/spf13/pflag"
)

func main() {
	config.NormalizeFlagNames(pflag.CommandLine)
	endpoint := pflag.String("endpoint", "", "Loopback status endpoint (auto-detected by default)")
	colorMode := pflag.String("color", "auto", "Color mode: auto, always, or never")
	versionFlag := pflag.Bool("version", false, "Print version information and exit")
	pflag.Parse()

	if *versionFlag {
		fmt.Println("Flow Dashboard")
		fmt.Println(version.Info())
		return
	}
	mode := strings.ToLower(*colorMode)
	if mode != "auto" && mode != "always" && mode != "never" {
		fmt.Fprintln(os.Stderr, "Invalid --color value; use auto, always, or never")
		os.Exit(2)
	}
	if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
		fmt.Fprintln(os.Stderr, "dashboard requires an interactive terminal; use kubectl or docker exec with -it")
		os.Exit(2)
	}
	client, err := dashboardui.NewClient(*endpoint)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid dashboard endpoint: %v\n", err)
		os.Exit(2)
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	colorEnabled := mode != "never" && (mode != "auto" || !noColor)
	model := dashboardui.NewModel(client, colorEnabled)
	options := []tea.ProgramOption{tea.WithFPS(2)}
	if mode == "never" || !colorEnabled {
		options = append(options, tea.WithColorProfile(colorprofile.ASCII))
	} else if mode == "always" {
		options = append(options, tea.WithColorProfile(colorprofile.TrueColor))
	}
	program := tea.NewProgram(model, options...)
	if _, err := program.Run(); err != nil && !errors.Is(err, tea.ErrInterrupted) {
		fmt.Fprintf(os.Stderr, "Dashboard stopped: %v\n", err)
		os.Exit(1)
	}
}
