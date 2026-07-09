package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"clash-jump-manager/internal/clash"
	"clash-jump-manager/internal/server"
)

//go:embed web/*
var embeddedWeb embed.FS

const defaultAddr = "127.0.0.1:8766"

func main() {
	webFS, err := fs.Sub(embeddedWeb, "web")
	if err != nil {
		log.Fatalf("load embedded web assets: %v", err)
	}
	exeDir, err := executableDir()
	if err != nil {
		log.Fatalf("resolve executable dir: %v", err)
	}
	app, err := server.New(server.Options{
		ConfigDir: clash.DefaultConfigDir(),
		StateFile: filepath.Join(exeDir, "state.json"),
		BackupDir: filepath.Join(exeDir, "backups"),
		StaticFS:  webFS,
	})
	if err != nil {
		log.Fatalf("initialize server: %v", err)
	}
	listener, err := net.Listen("tcp", defaultAddr)
	if err != nil {
		log.Fatalf("listen on %s: %v", defaultAddr, err)
	}
	url := "http://" + defaultAddr
	log.Printf("Clash Jump Manager started at %s", url)
	if shouldOpenBrowser() {
		go func() {
			time.Sleep(300 * time.Millisecond)
			if err := openBrowser(url); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}
	if err := http.Serve(listener, app.Handler()); err != nil {
		log.Fatalf("serve: %v", err)
	}
}

func executableDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.Dir(exe), nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func shouldOpenBrowser() bool {
	return os.Getenv("CLASH_JUMP_MANAGER_NO_BROWSER") == ""
}

func init() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	if runtime.GOOS != "windows" {
		fmt.Println("This tool is designed for Windows Clash Verge Rev, but the local web UI can run on this platform.")
	}
}
