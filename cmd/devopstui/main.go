// Command devopstui is a keyboard-driven TUI for Azure DevOps boards.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/config"
	"github.com/sveinungoverland/devopstui/internal/ui"
)

func main() {
	var (
		demo    = flag.Bool("demo", false, "run with in-memory demo data, no credentials needed")
		org     = flag.String("org", "", "organisation URL, e.g. https://dev.azure.com/contoso")
		project = flag.String("project", "", "project name")
		team    = flag.String("team", "", "team name")
		cfgPath = flag.String("config", config.Path(), "config file")
	)
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "config:", err)
		os.Exit(1)
	}
	patFromFile := cfg.PAT != "" && os.Getenv("AZURE_DEVOPS_EXT_PAT") == "" && os.Getenv("DEVOPSTUI_PAT") == ""
	if *org != "" {
		cfg.Org = *org
	}
	if *project != "" {
		cfg.Project = *project
	}
	if *team != "" {
		cfg.Team = *team
	}

	var client ado.Client
	if *demo {
		client = ado.NewFake()
		cfg.Org = "https://dev.azure.com/demo"
		if cfg.Project == "" {
			cfg.Project = "Platform"
		}
		if cfg.Team == "" {
			cfg.Team = "Team Blue"
		}
		*cfgPath = os.DevNull
	} else {
		if cfg.Org == "" || cfg.PAT == "" {
			fmt.Fprintln(os.Stderr, "need an organisation URL and a PAT.\n"+
				"  set AZURE_DEVOPS_ORG_URL and AZURE_DEVOPS_EXT_PAT, or put org/pat in "+*cfgPath+"\n"+
				"  or try: devopstui --demo")
			os.Exit(2)
		}
		sdk, err := ado.NewSDK(context.Background(), cfg.Org, cfg.PAT)
		if err != nil {
			fmt.Fprintln(os.Stderr, "connect:", err)
			os.Exit(1)
		}
		client = sdk
	}

	app := ui.New(client, cfg, *cfgPath, patFromFile)
	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
