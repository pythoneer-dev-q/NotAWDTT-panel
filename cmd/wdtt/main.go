package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"wdtt-panel"
	"wdtt-server"
)

func main() {
	noPanel := false
	nodeSync := ""
	nodeToken := ""
	filtered := make([]string, 0, len(os.Args))
	for i := 0; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch arg {
		case "-no-panel":
			noPanel = true
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
		case "-version", "--version":
			fmt.Println(panel.FormatPanelVersion())
			os.Exit(0)
		default:
			filtered = append(filtered, arg)
		}
	}
	os.Args = filtered

	if nodeSync != "" && nodeToken != "" {
		noPanel = true
		server.StartNodeSyncWorker(context.Background(), nodeSync, nodeToken)
	}

	if err := panel.BootstrapDB(); err != nil {
		log.Fatalf("[PANEL] bootstrap: %v", err)
	}

	if !noPanel {
		panel.InitUnifiedLogSink()
		go func() {
			if err := panel.Run(); err != nil {
				log.Fatalf("[PANEL] %v", err)
			}
		}()
		log.Println("[PANEL] веб-панель запущена в том же процессе")
	}

	server.Run()
}
