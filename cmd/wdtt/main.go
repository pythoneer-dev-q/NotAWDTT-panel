package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"strings"

	"wdtt-panel"
	"wdtt-server"
)

func main() {
	if len(os.Args) == 3 && os.Args[1] == "-reset-admin" {
		if err := panel.BootstrapDB(); err != nil {
			log.Fatal(err)
		}
		password, err := os.ReadFile("/dev/stdin")
		if err != nil {
			log.Fatal(err)
		}
		if err := panel.ResetAdminCredentials(os.Args[2], strings.TrimRight(string(password), "\r\n")); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Admin credentials updated")
		return
	}
	noPanel := false
	panelOnly := false
	nodeSync := ""
	nodeToken := ""
	nodeConfig := ""
	filtered := make([]string, 0, len(os.Args))
	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch arg {
		case "-no-panel":
			noPanel = true
		case "-panel-only":
			panelOnly = true
		case "-node-sync":
			if i+1 < len(os.Args) {
				nodeSync = os.Args[i+1]
				i++
			}
		case "-node-token":
			if i+1 < len(os.Args) {
				nodeToken = os.Args[i+1]
				i++
			}
		case "-node-config":
			if i+1 < len(os.Args) {
				nodeConfig = os.Args[i+1]
				i++
			}
		case "-version", "--version":
			fmt.Println(panel.FormatPanelVersion())
			os.Exit(0)
		default:
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered
	if nodeConfig != "" {
		data, err := os.ReadFile(nodeConfig)
		if err != nil {
			log.Fatal(err)
		}
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.SplitN(strings.TrimSpace(line), "=", 2)
			if len(parts) != 2 {
				continue
			}
			switch parts[0] {
			case "MASTER_URL":
				nodeSync = parts[1]
			case "NODE_TOKEN":
				nodeToken = parts[1]
			}
		}
		if nodeSync == "" || nodeToken == "" {
			log.Fatal("node config missing master URL or token")
		}
	}

	if nodeSync != "" && nodeToken != "" {
		noPanel = true
		server.StartNodeSyncWorker(context.Background(), nodeSync, nodeToken)
	} else {
		if err := panel.BootstrapDB(); err != nil {
			log.Fatalf("[PANEL] ERROR bootstrap: %v", err)
		}
	}

	if panelOnly {
		panel.InitUnifiedLogSink()
		panel.ConfigureLogLevel()
		if err := panel.Run(); err != nil {
			log.Fatalf("[PANEL] ERROR %v", err)
		}
		return
	}
	if !noPanel {
		panel.InitUnifiedLogSink()
		panel.ConfigureLogLevel()
		go func() {
			if err := panel.Run(); err != nil {
				log.Fatalf("[PANEL] ERROR %v", err)
			}
		}()
		log.Println("[PANEL] веб-панель запущена в том же процессе")
	}
	if noPanel {
		panel.ConfigureLogLevel()
	}
	server.Run()
}
