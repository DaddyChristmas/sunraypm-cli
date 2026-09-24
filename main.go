// ==============================================================================
// sunRayPM CLI Client & Terminal Developer Tool
//
// Copyright (c) 2026 sunRayPM Contributors & Kai Ali Kutsalcan.
// All rights reserved.
//
// Licensed under the MIT License. See LICENSE file in the repository root.
//
// ARCHITECTURAL REMARKS:
// This application is a thin, open-source client-side interface designed
// for interacting with the sunRayPM REST API.
// All scheduling leveling calculations, database states, and sensitive tenant
// workflows remain securely isolated on remote sunRayPM server environments.
// ==============================================================================

package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
	colorGold   = "\033[38;5;214m"
	colorCyan   = "\033[36m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorGray   = "\033[90m"
)

// Config holds CLI configuration persisted in ~/.sunray/config.json
type Config struct {
	BaseURL  string `json:"base_url"`
	Token    string `json:"token"`
	SpaceID  string `json:"space_id"`
	UserEmail string `json:"user_email,omitempty"`
}

func getConfigFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".sunray")
	_ = os.MkdirAll(dir, 0700)
	return filepath.Join(dir, "config.json")
}

func loadStoredConfig() Config {
	cfg := Config{
		BaseURL: "https://api.sunraypm.com",
	}
	file := getConfigFile()
	data, err := os.ReadFile(file)
	if err == nil {
		_ = json.Unmarshal(data, &cfg)
	}
	if cfg.BaseURL == "" || cfg.BaseURL == "http://localhost:8080" || cfg.BaseURL == "https://sunraypm.com" {
		cfg.BaseURL = "https://api.sunraypm.com"
	}
	return cfg
}

func saveConfig(cfg Config) error {
	file := getConfigFile()
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(file, data, 0600)
}

func getConfig() Config {
	cfg := loadStoredConfig()

	// Environment variable overrides
	if envURL := os.Getenv("SUNRAY_API_URL"); envURL != "" {
		cfg.BaseURL = envURL
	}
	if envToken := os.Getenv("SUNRAY_TOKEN"); envToken != "" {
		cfg.Token = envToken
	}
	if envSpace := os.Getenv("SUNRAY_SPACE_ID"); envSpace != "" {
		cfg.SpaceID = envSpace
	}

	return cfg
}

func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

func handleAuthLogin(cfg Config) {
	fmt.Printf("\n%s[sunRayPM] Browser Authentication%s\n", colorGold+colorBold, colorReset)

	// Pick available local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("%sError creating local auth listener:%s %v\n", colorRed, colorReset, err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port

	// Generate random state token
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	state := hex.EncodeToString(b)

	webURL := "https://sunraypm.com"
	if strings.Contains(cfg.BaseURL, "localhost") || strings.Contains(cfg.BaseURL, "127.0.0.1") {
		webURL = "http://localhost:5173"
	}
	authURL := fmt.Sprintf("%s/cli-auth?callback_port=%d&state=%s", webURL, port, state)

	tokenChan := make(chan struct {
		Token   string
		SpaceID string
		Email   string
		Error   string
	})

	server := &http.Server{}
	http.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		reqState := r.URL.Query().Get("state")
		if reqState != state {
			http.Error(w, "Invalid state token", http.StatusBadRequest)
			tokenChan <- struct {
				Token   string
				SpaceID string
				Email   string
				Error   string
			}{Error: "CSRF state verification failed"}
			return
		}

		tok := r.URL.Query().Get("token")
		spc := r.URL.Query().Get("space_id")
		email := r.URL.Query().Get("email")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head>
<title>sunRayPM CLI Authenticated</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #0f172a; color: #f8fafc; display: flex; align-items: center; justify-content: center; height: 100vh; margin: 0; }
.card { background: #1e293b; padding: 40px; border-radius: 16px; border: 1px solid #334155; text-align: center; box-shadow: 0 20px 25px -5px rgba(0,0,0,0.5); max-width: 420px; }
h1 { color: #f59e0b; margin-bottom: 8px; }
p { color: #94a3b8; font-size: 14px; }
.badge { display: inline-block; background: rgba(34,197,94,0.15); color: #22c55e; padding: 6px 12px; border-radius: 20px; font-weight: bold; font-size: 13px; margin: 16px 0; }
</style>
</head>
<body>
<div class="card">
  <h1>sunRayPM</h1>
  <div class="badge">[OK] Successfully Authenticated</div>
  <p>Your terminal session is now connected to sunRayPM.</p>
  <p style="margin-top: 24px; font-size: 12px; color: #64748b;">You can safely close this browser window and return to your terminal.</p>
</div>
</body>
</html>`)

		tokenChan <- struct {
			Token   string
			SpaceID string
			Email   string
			Error   string
		}{
			Token:   tok,
			SpaceID: spc,
			Email:   email,
		}
	})

	go func() {
		_ = server.Serve(listener)
	}()

	fmt.Println("Opening your default browser to complete authentication...")
	fmt.Printf("If it doesn't open automatically, please open:\n%s%s%s\n\n", colorCyan, authURL, colorReset)
	_ = openBrowser(authURL)

	fmt.Print("Waiting for authorization in browser...")

	select {
	case res := <-tokenChan:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)

		if res.Error != "" {
			fmt.Printf("\n%sAuthentication failed:%s %s\n", colorRed, colorReset, res.Error)
			return
		}

		if res.Token == "" {
			fmt.Printf("\n%sAuthentication failed: no token received%s\n", colorRed, colorReset)
			return
		}

		cfg.Token = res.Token
		if res.SpaceID != "" {
			cfg.SpaceID = res.SpaceID
		}
		if res.Email != "" {
			cfg.UserEmail = res.Email
		}

		if err := saveConfig(cfg); err != nil {
			fmt.Printf("\n%sFailed to persist token:%s %v\n", colorRed, colorReset, err)
			return
		}

		fmt.Printf("\n%s[OK] Authentication successful!%s\n", colorGreen+colorBold, colorReset)
		if cfg.UserEmail != "" {
			fmt.Printf("Logged in as: %s%s%s\n", colorBold, cfg.UserEmail, colorReset)
		}
		if cfg.SpaceID != "" {
			fmt.Printf("Active Space: %s%s%s\n", colorGold, cfg.SpaceID, colorReset)
		}
		fmt.Printf("Credentials saved to: %s\n\n", getConfigFile())

	case <-time.After(120 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		fmt.Printf("\n%sAuthentication timed out (120s).%s Please try again.\n", colorRed, colorReset)
	}
}

func handleAuthLogout() {
	file := getConfigFile()
	_ = os.Remove(file)
	fmt.Printf("%s[OK] Logged out successfully.%s Saved credentials removed from %s\n", colorGreen, colorReset, file)
}

func handleAuthStatus(cfg Config) {
	fmt.Printf("\n%s[sunRayPM] Authentication Status%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("API Server:  %s%s%s\n", colorCyan, cfg.BaseURL, colorReset)
	if cfg.Token != "" {
		fmt.Printf("Status:      %sLogged In%s\n", colorGreen, colorReset)
		if cfg.UserEmail != "" {
			fmt.Printf("Account:     %s%s%s\n", colorBold, cfg.UserEmail, colorReset)
		}
		tokenPreview := cfg.Token
		if len(tokenPreview) > 16 {
			tokenPreview = tokenPreview[:8] + "..." + tokenPreview[len(tokenPreview)-8:]
		}
		fmt.Printf("Token:       %s%s%s\n", colorGray, tokenPreview, colorReset)
		if cfg.SpaceID != "" {
			fmt.Printf("Space ID:    %s%s%s\n", colorGold, cfg.SpaceID, colorReset)
		}
	} else {
		fmt.Printf("Status:      %sNot Logged In%s\n", colorYellow, colorReset)
		fmt.Println("Run `sunray auth login` or `sunray login` to authenticate.")
	}
	fmt.Println()
}

const AppVersion = "v0.1.0"

func handleUpdate() {
	fmt.Printf("\n%s[sunRayPM] Checking for CLI updates...%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("Current Version: %s%s%s\n", colorCyan, AppVersion, colorReset)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/DaddyChristmas/sunraypm-cli/releases/latest", nil)
	if err != nil {
		fmt.Printf("%sError creating update request:%s %v\n", colorRed, colorReset, err)
		return
	}
	req.Header.Set("User-Agent", "sunraypm-cli")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("%sError checking for updates:%s %v\n", colorRed, colorReset, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("%sNo public releases published on GitHub yet (or API rate limited).%s\n", colorYellow, colorReset)
		fmt.Println("To update from source, run: go install github.com/DaddyChristmas/sunraypm-cli@latest")
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Printf("%sError parsing release data:%s %v\n", colorRed, colorReset, err)
		return
	}

	if release.TagName == AppVersion {
		fmt.Printf("%s[OK] You are already on the latest version!%s (%s)\n\n", colorGreen, colorReset, AppVersion)
		return
	}

	fmt.Printf("New version available: %s%s%s (Current: %s)\n", colorGreen+colorBold, release.TagName, colorReset, AppVersion)

	targetAsset := fmt.Sprintf("sunray-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, targetAsset) {
			downloadURL = asset.BrowserDownloadURL
			break
		}
	}

	if downloadURL == "" {
		fmt.Printf("%sPrebuilt binary for %s-%s not found in release %s.%s\n", colorYellow, runtime.GOOS, runtime.GOARCH, release.TagName, colorReset)
		fmt.Println("You can update via: go install github.com/DaddyChristmas/sunraypm-cli@latest")
		return
	}

	fmt.Printf("Downloading %s...\n", downloadURL)
	binResp, err := client.Get(downloadURL)
	if err != nil {
		fmt.Printf("%sDownload failed:%s %v\n", colorRed, colorReset, err)
		return
	}
	defer binResp.Body.Close()

	execPath, err := os.Executable()
	if err != nil {
		fmt.Printf("%sError locating current executable:%s %v\n", colorRed, colorReset, err)
		return
	}

	tmpFile := execPath + ".tmp"
	out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		fmt.Printf("%sFailed to create temporary binary file (try with sudo):%s %v\n", colorRed, colorReset, err)
		return
	}
	_, err = io.Copy(out, binResp.Body)
	out.Close()
	if err != nil {
		_ = os.Remove(tmpFile)
		fmt.Printf("%sError writing binary:%s %v\n", colorRed, colorReset, err)
		return
	}

	if err := os.Rename(tmpFile, execPath); err != nil {
		_ = os.Remove(tmpFile)
		fmt.Printf("%sError replacing binary (try with sudo):%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s[OK] Successfully upgraded sunRayPM CLI to %s!%s\n\n", colorGreen+colorBold, release.TagName, colorReset)
}

func makeRequest(cfg Config, method, path string, body interface{}) ([]byte, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(jsonBytes)
	}

	url := fmt.Sprintf("%s%s", cfg.BaseURL, path)
	req, err := http.NewRequest(method, url, bodyReader)

	if err != nil {
		return nil, err
	}

	if cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// Task model helper for CLI parsing
type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Name         string   `json:"name"`
	Type         string   `json:"type"`
	Progress     float64  `json:"progress"`
	DurationDays float64  `json:"duration_days"`
	StartPlanned string   `json:"start_planned"`
	FinishPlan   string   `json:"finish_planned"`
	CostPlanned  float64  `json:"cost_planned"`
	CostActual   float64  `json:"cost_actual"`
	ParentID     *string  `json:"parent_id,omitempty"`
	Assignees    []string `json:"assignees,omitempty"`
}

func (t Task) DisplayTitle() string {
	if t.Title != "" {
		return t.Title
	}
	return t.Name
}

func fetchSpaceTasks(cfg Config, spaceID string) ([]Task, error) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		return nil, fmt.Errorf("no active workspace selected (run `spaces switch <@name or ID>`)")
	}

	// Find tenant ID for space
	tenantID := ""
	spaces, err := fetchAllSpaces(cfg)
	if err == nil {
		for _, s := range spaces {
			if s.ID == spaceID {
				tenantID = s.TenantID
				break
			}
		}
	}

	url := fmt.Sprintf("/api/containers?space_id=%s", spaceID)
	if tenantID != "" {
		url = fmt.Sprintf("/api/containers?tenant_id=%s&space_id=%s", tenantID, spaceID)
	}

	data, err := makeRequest(cfg, "GET", url, nil)
	if err != nil {
		// Fallback to /api/containers/tree
		if tenantID != "" {
			data, err = makeRequest(cfg, "GET", fmt.Sprintf("/api/containers/tree?tenant_id=%s&space_id=%s", tenantID, spaceID), nil)
		}
		if err != nil {
			return nil, err
		}
	}

	var resp struct {
		Data []Task `json:"data"`
	}
	if err := json.Unmarshal(data, &resp); err == nil && len(resp.Data) > 0 {
		return resp.Data, nil
	}

	var tasks []Task
	if err := json.Unmarshal(data, &tasks); err == nil {
		return tasks, nil
	}

	return nil, fmt.Errorf("failed to parse task data")
}

// resolveTaskID resolves "@xxxx", "@task_name", or plain ID
func resolveTaskID(ref string, tasks []Task) string {
	clean := strings.TrimPrefix(ref, "@")
	if clean == "" {
		return ""
	}

	// 1. Exact ID match
	for _, t := range tasks {
		if t.ID == clean {
			return t.ID
		}
	}

	// 2. Prefix ID match
	for _, t := range tasks {
		if strings.HasPrefix(t.ID, clean) {
			return t.ID
		}
	}

	// 3. Exact or case-insensitive Title match
	cleanLower := strings.ToLower(clean)
	for _, t := range tasks {
		if strings.ToLower(t.DisplayTitle()) == cleanLower {
			return t.ID
		}
	}

	// 4. Substring Title match
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.DisplayTitle()), cleanLower) {
			return t.ID
		}
	}

	return clean
}

// filterTasksByQuery returns matching tasks for autocomplete dropdown
func filterTasksByQuery(query string, tasks []Task) []Task {
	clean := strings.ToLower(strings.TrimPrefix(query, "@"))
	var matches []Task
	for _, t := range tasks {
		if clean == "" || strings.HasPrefix(strings.ToLower(t.ID), clean) || strings.Contains(strings.ToLower(t.DisplayTitle()), clean) {
			matches = append(matches, t)
			if len(matches) >= 6 {
				break
			}
		}
	}
	return matches
}

// renderTaskPreviewBox renders a rich terminal preview card for a task
func renderTaskPreviewBox(t Task) {
	idShort := t.ID
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}
	progBar := renderMiniProgressBar(int(t.Progress))
	statusColor := colorYellow
	statusText := "IN PROGRESS"
	if t.Progress >= 100 {
		statusColor = colorGreen
		statusText = "DONE"
	} else if t.Progress == 0 {
		statusColor = colorGray
		statusText = "NOT STARTED"
	}

	fmt.Printf("   ┌── %s@%s%s ──────────────────────────────────────────────\n", colorGold, idShort, colorReset)
	fmt.Printf("   │ %s%s%s\n", colorBold, t.DisplayTitle(), colorReset)
	fmt.Printf("   │ Status:   %s[%s]%s  Progress: %s (%.0f%%)\n", statusColor, statusText, colorReset, progBar, t.Progress)
	fmt.Printf("   │ Duration: %.0fd  •  Cost: $%.0f\n", t.DurationDays, t.CostPlanned)
	if t.StartPlanned != "" || t.FinishPlan != "" {
		start := t.StartPlanned
		if len(start) > 10 {
			start = start[:10]
		}
		finish := t.FinishPlan
		if len(finish) > 10 {
			finish = finish[:10]
		}
		fmt.Printf("   │ Dates:    %s → %s\n", start, finish)
	}
	fmt.Printf("   └─────────────────────────────────────────────────────────\n")
}

func renderMiniProgressBar(pct int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := pct / 10
	empty := 10 - filled
	return fmt.Sprintf("[%s%s%s%s]", colorGreen, strings.Repeat("█", filled), colorGray, strings.Repeat("░", empty))
}

func printHelp() {
	fmt.Printf("%s[sunRayPM] CLI - Developer Tool & Interactive Terminal%s\n\n", colorGold+colorBold, colorReset)
	fmt.Println("Usage:")
	fmt.Println("  sunray [command] [flags]")

	fmt.Println("  sunray repl                       Launch interactive REPL shell")
	fmt.Println()
	fmt.Println("Available Commands:")
	fmt.Println("  auth login                        Log in via browser window (saves to ~/.sunray/config.json)")
	fmt.Println("  auth logout                       Log out and remove local credentials")
	fmt.Println("  auth status                       Display current authentication status & workspace")
	fmt.Println("  spaces list                       List all workspaces (--json supported)")
	fmt.Println("  tasks list   --space-id <ID>      List tasks in a space (--json supported)")
	fmt.Println("  tasks create --space-id <ID> --name <Name> [--duration <Days>] [--parent @task]")
	fmt.Println("  tasks update @task [--name <Name>] [--progress <Pct>] [--duration <Days>]")
	fmt.Println("  tasks delete @task                Delete a task")
	fmt.Println("  draw-kanban  [@container]         Render 3-column ASCII Kanban board (/draw-kanban)")
	fmt.Println("  draw-gantt   [@container]         Render ASCII Gantt schedule timeline (/draw-gantt)")
	fmt.Println("  draw-tree    [@container]         Render ASCII hierarchy DAG tree (/draw-tree)")
	fmt.Println("  sunny                             Show Sunny project pet mascot and status")
	fmt.Println("  deps add     --pred @t1 --succ @t2 Create DAG dependency edge")
	fmt.Println("  evm          --space-id <ID>      Show Earned Value Management metrics")
	fmt.Println("  report       --space-id <ID>      Generate Executive Audit Markdown report")
	fmt.Println("  update, upgrade                   Self-update CLI to latest GitHub release")
	fmt.Println("  version                           Show CLI version")

	fmt.Println()
	fmt.Println("Task Reference Syntax:")
	fmt.Printf("  Use %s@<task_id>%s or %s@<task_name>%s (e.g., %s@t1%s or %s@\"Frontend Build\"%s)\n\n", colorCyan, colorReset, colorCyan, colorReset, colorCyan, colorReset, colorCyan, colorReset)
	fmt.Println("Environment Variables:")
	fmt.Println("  SUNRAY_API_URL   API base URL (default: https://api.sunraypm.com)")
	fmt.Println("  SUNRAY_TOKEN     ZITADEL Bearer token for authentication")
	fmt.Println("  SUNRAY_SPACE_ID  Default space ID for commands")
}

type SpaceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TenantID    string `json:"tenant_id"`
	TenantName  string `json:"tenant_name"`
}

func fetchAllSpaces(cfg Config) ([]SpaceInfo, error) {
	tenantData, err := makeRequest(cfg, "GET", "/api/tenants", nil)
	if err != nil {
		return nil, err
	}

	var tenantResp struct {
		Data []map[string]interface{} `json:"data"`
	}
	_ = json.Unmarshal(tenantData, &tenantResp)

	var allSpaces []SpaceInfo

	if len(tenantResp.Data) == 0 {
		data, err := makeRequest(cfg, "GET", "/api/spaces", nil)
		if err == nil {
			_ = json.Unmarshal(data, &allSpaces)
		}
	} else {
		for _, tenant := range tenantResp.Data {
			tID, _ := tenant["id"].(string)
			tName, _ := tenant["name"].(string)
			if tID == "" {
				continue
			}
			spacesData, err := makeRequest(cfg, "GET", fmt.Sprintf("/api/spaces?tenant_id=%s", tID), nil)
			if err != nil {
				continue
			}
			var spaces []SpaceInfo
			if err := json.Unmarshal(spacesData, &spaces); err == nil {
				for _, s := range spaces {
					s.TenantName = tName
					s.TenantID = tID
					allSpaces = append(allSpaces, s)
				}
			}
		}
	}

	return allSpaces, nil
}

func resolveSpace(query string, spaces []SpaceInfo) (SpaceInfo, bool) {
	clean := strings.TrimSpace(strings.TrimPrefix(query, "@"))
	if clean == "" {
		return SpaceInfo{}, false
	}
	cleanLower := strings.ToLower(clean)

	// 1. Exact ID
	for _, s := range spaces {
		if strings.EqualFold(s.ID, clean) {
			return s, true
		}
	}

	// 2. Exact Name
	for _, s := range spaces {
		if strings.EqualFold(s.Name, clean) {
			return s, true
		}
	}

	// 3. Prefix ID
	for _, s := range spaces {
		if strings.HasPrefix(strings.ToLower(s.ID), cleanLower) {
			return s, true
		}
	}

	// 4. Prefix or Substring Name
	for _, s := range spaces {
		if strings.HasPrefix(strings.ToLower(s.Name), cleanLower) {
			return s, true
		}
	}
	for _, s := range spaces {
		if strings.Contains(strings.ToLower(s.Name), cleanLower) {
			return s, true
		}
	}

	return SpaceInfo{}, false
}

func handleSpacesSwitch(cfg *Config, spaceRef string) {
	spaces, err := fetchAllSpaces(*cfg)
	if err != nil {
		fmt.Printf("%sError fetching spaces:%s %v\n", colorRed, colorReset, err)
		return
	}

	if len(spaces) == 0 {
		fmt.Println("No workspaces found. Create one on https://sunraypm.com first.")
		return
	}

	if spaceRef == "" {
		fmt.Println("Usage: spaces switch <@name or space_id>")
		fmt.Printf("\n%sAvailable Workspaces:%s\n", colorGold, colorReset)
		for _, s := range spaces {
			active := ""
			if s.ID == cfg.SpaceID {
				active = colorGreen + " [Active]" + colorReset
			}
			fmt.Printf("  • %s%-20s%s (@%s)%s\n", colorBold, s.Name, colorReset, s.ID, active)
		}
		fmt.Println()
		return
	}

	target, found := resolveSpace(spaceRef, spaces)
	if !found {
		fmt.Printf("%sNo workspace matching '%s'%s\n\n", colorRed, spaceRef, colorReset)
		fmt.Println("Available Workspaces:")
		for _, s := range spaces {
			fmt.Printf("  • %s (@%s)\n", s.Name, s.ID)
		}
		fmt.Println()
		return
	}

	cfg.SpaceID = target.ID
	if err := saveConfig(*cfg); err != nil {
		fmt.Printf("%sWarning: failed to persist config:%s %v\n", colorYellow, colorReset, err)
	}

	fmt.Printf("%s[OK]%s Active workspace switched to: %s%s%s (@%s)\n", colorGreen, colorReset, colorGold+colorBold, target.Name, colorReset, target.ID)
	if target.TenantName != "" {
		fmt.Printf("   Organization: %s%s%s\n", colorCyan, target.TenantName, colorReset)
	}
}

func handleSpacesList(cfg Config, jsonOut bool) {
	spaces, err := fetchAllSpaces(cfg)
	if err != nil {
		fmt.Printf("%sError fetching workspaces:%s %v\n", colorRed, colorReset, err)
		return
	}

	if jsonOut {
		b, _ := json.MarshalIndent(spaces, "", "  ")
		fmt.Println(string(b))
		return
	}

	if len(spaces) == 0 {
		fmt.Println("No workspaces found. Create one in the web dashboard or check permissions.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sSPACE ID\tNAME\tORGANIZATION\tSTATUS%s\n", colorBold, colorReset)
	for _, s := range spaces {
		orgName := s.TenantName
		if orgName == "" {
			orgName = s.TenantID
		}
		activeTag := ""
		if s.ID == cfg.SpaceID {
			activeTag = colorGreen + "[Active]" + colorReset
		}
		fmt.Fprintf(w, "%s%s%s\t%s\t%s\t%s\n", colorCyan, s.ID, colorReset, s.Name, orgName, activeTag)
	}
	w.Flush()
	fmt.Println()
	fmt.Println("To switch active workspace, run: spaces switch <@name or ID>")
}

func handleTasksList(cfg Config, spaceID string, jsonOut bool) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		fmt.Printf("%sError:%s space ID is required (run `spaces list` and `spaces switch <ID>`)\n", colorRed, colorReset)
		return
	}

	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	if jsonOut {
		b, _ := json.MarshalIndent(tasks, "", "  ")
		fmt.Println(string(b))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sID\tTITLE\tPROGRESS\tDURATION\tSTART\tFINISH\tCOST (PLANNED)%s\n", colorBold, colorReset)
	for _, t := range tasks {
		idShort := t.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		start := t.StartPlanned
		if len(start) > 10 {
			start = start[:10]
		}
		finish := t.FinishPlan
		if len(finish) > 10 {
			finish = finish[:10]
		}
		progColor := colorGray
		if t.Progress >= 100 {
			progColor = colorGreen
		} else if t.Progress > 0 {
			progColor = colorCyan
		}

		fmt.Fprintf(w, "@%s\t%s\t%s%.0f%%%s\t%.0fd\t%s\t%s\t$%.0f\n", idShort, t.DisplayTitle(), progColor, t.Progress, colorReset, t.DurationDays, start, finish, t.CostPlanned)
	}
	w.Flush()
}

func handleTasksCreate(cfg Config, spaceID, name string, duration int, parentRef string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" || name == "" {
		fmt.Printf("%sError:%s --space-id and --name are required\n", colorRed, colorReset)
		return
	}

	parentID := ""
	if parentRef != "" {
		tasks, _ := fetchSpaceTasks(cfg, spaceID)
		parentID = resolveTaskID(parentRef, tasks)
	}

	payload := map[string]interface{}{
		"space_id":      spaceID,
		"title":         name,
		"name":          name,
		"type":          "task",
		"duration_days": duration,
		"progress":      0,
	}
	if parentID != "" {
		payload["parent_id"] = parentID
	}

	data, err := makeRequest(cfg, "POST", "/api/containers", payload)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	var res map[string]interface{}
	json.Unmarshal(data, &res)
	id, _ := res["id"].(string)
	fmt.Printf("%s[OK]%s Task created successfully! ID: %s@%s%s\n", colorGreen, colorReset, colorGold, id, colorReset)
}

func handleTasksUpdate(cfg Config, spaceID, taskRef, name string, progress, duration int) {
	if taskRef == "" {
		fmt.Printf("%sError:%s Task reference (@task) is required\n", colorRed, colorReset)
		return
	}

	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	targetID := resolveTaskID(taskRef, tasks)

	payload := map[string]interface{}{}
	if name != "" {
		payload["title"] = name
		payload["name"] = name
	}
	if progress >= 0 {
		payload["progress"] = progress
	}
	if duration > 0 {
		payload["duration_days"] = duration
	}

	_, err := makeRequest(cfg, "PUT", fmt.Sprintf("/api/containers/%s", targetID), payload)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s[OK]%s Task @%s updated successfully!\n", colorGreen, colorReset, targetID)
}

func handleTasksDelete(cfg Config, spaceID, taskRef string) {
	if taskRef == "" {
		fmt.Printf("%sError:%s Task reference (@task) is required\n", colorRed, colorReset)
		return
	}
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	targetID := resolveTaskID(taskRef, tasks)

	_, err := makeRequest(cfg, "DELETE", fmt.Sprintf("/api/containers/%s", targetID), nil)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s[OK]%s Task @%s deleted successfully!\n", colorGreen, colorReset, targetID)
}

func handleDepsAdd(cfg Config, spaceID, predRef, succRef string) {
	if predRef == "" || succRef == "" {
		fmt.Printf("%sError:%s --pred and --succ are required\n", colorRed, colorReset)
		return
	}
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	predID := resolveTaskID(predRef, tasks)
	succID := resolveTaskID(succRef, tasks)

	payload := map[string]interface{}{
		"predecessor_id": predID,
		"successor_id":   succID,
	}

	_, err := makeRequest(cfg, "POST", "/api/dependencies", payload)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s[OK]%s Dependency linked: @%s ➔ @%s\n", colorGreen, colorReset, predID, succID)
}

func handleEVM(cfg Config, spaceID string, jsonOut bool) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		fmt.Printf("%sError:%s --space-id is required\n", colorRed, colorReset)
		return
	}

	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	var BAC, EV, AC, PV float64
	now := time.Now()

	for _, t := range tasks {
		BAC += t.CostPlanned
		AC += t.CostActual
		EV += (t.CostPlanned * (t.Progress / 100.0))

		if t.StartPlanned != "" {
			if startT, sErr := time.Parse(time.RFC3339, t.StartPlanned); sErr == nil {
				finishT := startT.Add(time.Duration(t.DurationDays) * 24 * time.Hour)
				if t.FinishPlan != "" {
					if fT, fErr := time.Parse(time.RFC3339, t.FinishPlan); fErr == nil {
						finishT = fT
					}
				}
				if now.After(finishT) {
					PV += t.CostPlanned
				} else if now.After(startT) {
					dur := finishT.Sub(startT).Seconds()
					if dur > 0 {
						PV += t.CostPlanned * (now.Sub(startT).Seconds() / dur)
					}
				}
			}
		}
	}

	CV := EV - AC
	SV := EV - PV
	CPI := 1.0
	if AC > 0 {
		CPI = EV / AC
	}
	SPI := 1.0
	if PV > 0 {
		SPI = EV / PV
	}
	EAC := BAC
	if CPI > 0 {
		EAC = BAC / CPI
	}
	VAC := BAC - EAC

	if jsonOut {
		evmMap := map[string]float64{
			"BAC": BAC, "PV": PV, "EV": EV, "AC": AC,
			"CV": CV, "SV": SV, "CPI": CPI, "SPI": SPI,
			"EAC": EAC, "VAC": VAC,
		}
		b, _ := json.MarshalIndent(evmMap, "", "  ")
		fmt.Println(string(b))
		return
	}

	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("[EVM] %sEarned Value Management%s  Space: %s\n", colorBold, colorReset, spaceID)
	fmt.Println("══════════════════════════════════════════════════")
	fmt.Printf("Budget at Completion (BAC):   $%.2f\n", BAC)
	fmt.Printf("Planned Value (PV):          $%.2f\n", PV)
	fmt.Printf("Earned Value (EV):           $%.2f\n", EV)
	fmt.Printf("Actual Cost (AC):            $%.2f\n", AC)
	fmt.Printf("Cost Variance (CV):          $%.2f\n", CV)
	fmt.Printf("Schedule Variance (SV):      $%.2f\n", SV)
	fmt.Printf("Cost Performance Index (CPI): %s%.2f%s (EV/AC)\n", getIndexColor(CPI), CPI, colorReset)
	fmt.Printf("Schedule Perf. Index (SPI):  %s%.2f%s (EV/PV)\n", getIndexColor(SPI), SPI, colorReset)
	fmt.Printf("Estimate at Completion (EAC):$%.2f\n", EAC)
	fmt.Printf("Variance at Completion (VAC):$%.2f\n", VAC)
	fmt.Println("══════════════════════════════════════════════════")
}

func getIndexColor(idx float64) string {
	if idx >= 1.0 {
		return colorGreen
	} else if idx >= 0.85 {
		return colorYellow
	}
	return colorRed
}

func handleReport(cfg Config, spaceID string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		fmt.Printf("%sError:%s --space-id is required\n", colorRed, colorReset)
		return
	}

	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	var totalPlannedCost, totalActualCost, totalDays, completedDays float64
	var completedCount, overdueCount int
	now := time.Now()

	for _, t := range tasks {
		totalPlannedCost += t.CostPlanned
		totalActualCost += t.CostActual
		totalDays += t.DurationDays
		completedDays += (t.DurationDays * (t.Progress / 100.0))
		if t.Progress >= 100 {
			completedCount++
		}
		if t.FinishPlan != "" {
			if fT, err := time.Parse(time.RFC3339, t.FinishPlan); err == nil && now.After(fT) && t.Progress < 100 {
				overdueCount++
			}
		}
	}

	fmt.Println("# [sunRayPM] Executive Project Status Report")
	fmt.Printf("**Generated:** %s | **Space ID:** `%s`\n\n", time.Now().Format("2006-01-02 15:04 MST"), spaceID)
	fmt.Println("## 1. Executive Summary")
	fmt.Printf("- **Total Scope:** %d containers & tasks\n", len(tasks))
	fmt.Printf("- **Completion Rate:** %.1f%% (%d of %d tasks completed)\n", (completedDays/max(1, totalDays))*100, completedCount, len(tasks))
	fmt.Printf("- **Overdue Tasks (Slippage):** %d tasks\n", overdueCount)
	fmt.Printf("- **Financials:** $%.0f Actual Spend / $%.0f Planned Budget\n\n", totalActualCost, totalPlannedCost)
	fmt.Println("## 2. Key Task Progress")
	fmt.Println("| Task ID | Title | Duration | Progress | Planned Cost | Status |")
	fmt.Println("| :--- | :--- | :--- | :--- | :--- | :--- |")
	for _, t := range tasks {
		status := "In Progress"
		if t.Progress >= 100 {
			status = "Done"
		} else if t.Progress == 0 {
			status = "Not Started"
		}
		fmt.Printf("| `@%s` | %s | %.0fd | %.0f%% | $%.0f | %s |\n", t.ID, t.DisplayTitle(), t.DurationDays, t.Progress, t.CostPlanned, status)
	}
}

// ─── ASCII VISUALIZERS & PROGRAMMER GIMMICKS ──────────────────────────────

func handleDrawKanban(cfg Config, spaceID string, containerQuery string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		fmt.Printf("%sError:%s space ID required (use --space-id or SUNRAY_SPACE_ID)\n", colorRed, colorReset)
		return
	}

	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError fetching tasks:%s %v\n", colorRed, colorReset, err)
		return
	}

	// If container query provided (e.g. @containerxyz), filter to container + descendants
	parentID := ""
	if containerQuery != "" {
		parentID = resolveTaskID(containerQuery, tasks)
		var scoped []Task
		for _, t := range tasks {
			if t.ID == parentID || (t.ParentID != nil && *t.ParentID == parentID) {
				scoped = append(scoped, t)
			}
		}
		if len(scoped) > 0 {
			tasks = scoped
		}
	}

	var todo, inProg, done []Task
	for _, t := range tasks {
		if t.Progress >= 100 {
			done = append(done, t)
		} else if t.Progress > 0 {
			inProg = append(inProg, t)
		} else {
			todo = append(todo, t)
		}
	}

	fmt.Printf("\n%s[sunRayPM] Terminal Kanban Board%s", colorGold+colorBold, colorReset)
	if containerQuery != "" {
		fmt.Printf(" (Scope: %s%s%s)", colorCyan, containerQuery, colorReset)
	}
	fmt.Printf(" • Total: %d tasks\n\n", len(tasks))

	renderKanbanColumn("[ ] TO DO", todo, colorGray)
	renderKanbanColumn("[~] IN PROGRESS", inProg, colorYellow)
	renderKanbanColumn("[x] DONE", done, colorGreen)
	fmt.Println()
}

func renderKanbanColumn(title string, tasks []Task, headerColor string) {
	fmt.Printf("%s=== %s (%d) =========================================%s\n", headerColor+colorBold, title, len(tasks), colorReset)
	if len(tasks) == 0 {
		fmt.Printf("   %s(No tasks)%s\n\n", colorGray, colorReset)
		return
	}

	for _, t := range tasks {
		idShort := t.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		progBar := renderMiniProgressBar(int(t.Progress))
		titleStr := t.DisplayTitle()
		if len(titleStr) > 35 {
			titleStr = titleStr[:32] + "..."
		}

		fmt.Printf("  ┌─ %s@%-8s%s %s%s%s\n", colorCyan, idShort, colorReset, colorBold, titleStr, colorReset)
		fmt.Printf("  │  Dur: %2.0fd  •  Cost: $%4.0f  •  %s %3.0f%%\n", t.DurationDays, t.CostPlanned, progBar, t.Progress)
		if t.StartPlanned != "" || t.FinishPlan != "" {
			s := t.StartPlanned
			if len(s) > 10 {
				s = s[:10]
			}
			f := t.FinishPlan
			if len(f) > 10 {
				f = f[:10]
			}
			fmt.Printf("  │  Dates: %s → %s\n", s, f)
		}
		fmt.Printf("  └─────────────────────────────────────────────────────\n")
	}
	fmt.Println()
}

func handleDrawGantt(cfg Config, spaceID string, containerQuery string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		fmt.Printf("%sError:%s space ID required\n", colorRed, colorReset)
		return
	}

	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	if containerQuery != "" {
		parentID := resolveTaskID(containerQuery, tasks)
		var scoped []Task
		for _, t := range tasks {
			if t.ID == parentID || (t.ParentID != nil && *t.ParentID == parentID) {
				scoped = append(scoped, t)
			}
		}
		if len(scoped) > 0 {
			tasks = scoped
		}
	}

	fmt.Printf("\n%s[sunRayPM] Terminal Gantt Timeline%s (Tasks: %d)\n\n", colorGold+colorBold, colorReset, len(tasks))
	fmt.Printf(" %-24s | %sDays (1 to 20)%s\n", "TASK / CONTAINER", colorGray, colorReset)
	fmt.Printf(" %-24s | 01  03  05  07  09  11  13  15  17  19\n", "------------------------")
	fmt.Printf("--------------------------+-----------------------------------------\n")

	for i, t := range tasks {
		name := t.DisplayTitle()
		if len(name) > 20 {
			name = name[:18] + ".."
		}
		prefix := fmt.Sprintf("@%-6s %s", t.ID[:minInt(6, len(t.ID))], name)

		// Calculate visual bar offset and length
		dur := int(t.DurationDays)
		if dur < 1 {
			dur = 1
		}
		offset := (i * 2) % 15
		barLen := minInt(dur*2, 20)

		leadSpace := strings.Repeat(" ", offset*2)
		var bar string
		if t.Type == "milestone" {
			bar = fmt.Sprintf("%s[M] (Milestone)%s", colorGold, colorReset)
		} else if t.Progress >= 100 {
			bar = fmt.Sprintf("%s[%s]%s", colorGreen, strings.Repeat("=", maxInt(1, barLen)), colorReset)
		} else if t.Progress > 0 {
			filled := (barLen * int(t.Progress)) / 100
			unfilled := barLen - filled
			bar = fmt.Sprintf("%s[%s%s%s]%s", colorYellow, strings.Repeat("█", filled), colorGray, strings.Repeat("░", unfilled), colorReset)
		} else {
			bar = fmt.Sprintf("%s[%s]%s", colorGray, strings.Repeat("-", maxInt(1, barLen)), colorReset)
		}

		fmt.Printf(" %-24s | %s%s\n", prefix, leadSpace, bar)
	}
	fmt.Printf("--------------------------+-----------------------------------------\n\n")
}

func handleDrawTree(cfg Config, spaceID string, containerQuery string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("\n%s[sunRayPM] Container & Task Tree Structure%s\n\n", colorGold+colorBold, colorReset)

	// Map parent to children
	childrenMap := make(map[string][]Task)
	var rootTasks []Task
	for _, t := range tasks {
		if t.ParentID == nil || *t.ParentID == "" {
			rootTasks = append(rootTasks, t)
		} else {
			childrenMap[*t.ParentID] = append(childrenMap[*t.ParentID], t)
		}
	}

	if len(rootTasks) == 0 {
		rootTasks = tasks
	}

	for _, root := range rootTasks {
		renderTreeNode(root, childrenMap, "", true)
	}
	fmt.Println()
}

func renderTreeNode(node Task, childrenMap map[string][]Task, indent string, isLast bool) {
	marker := "├── "
	if isLast {
		marker = "└── "
	}

	typeIcon := "[T]"
	if node.Type == "milestone" {
		typeIcon = "[M]"
	} else if node.Type == "project" || node.Type == "kanban" {
		typeIcon = "[C]"
	}

	progColor := colorGray
	if node.Progress >= 100 {
		progColor = colorGreen
	} else if node.Progress > 0 {
		progColor = colorYellow
	}

	fmt.Printf("%s%s%s %s%s%s %s(@%s - %.0f%%)%s\n",
		indent, marker, typeIcon, colorBold, node.DisplayTitle(), colorReset, progColor, node.ID[:minInt(6, len(node.ID))], node.Progress, colorReset)

	children := childrenMap[node.ID]
	for i, ch := range children {
		nextIndent := indent + "│   "
		if isLast {
			nextIndent = indent + "    "
		}
		renderTreeNode(ch, childrenMap, nextIndent, i == len(children)-1)
	}
}

func handleSunny(cfg Config, spaceID string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	done := 0
	inProg := 0
	for _, t := range tasks {
		if t.Progress >= 100 {
			done++
		} else if t.Progress > 0 {
			inProg++
		}
	}

	fmt.Println()
	fmt.Printf("%s      \\   /      %s\n", colorGold, colorReset)
	fmt.Printf("%s       .-.       %s  %sSunny (Project Companion):%s\n", colorGold, colorReset, colorBold, colorReset)
	fmt.Printf("%s    ― ( O O ) ―  %s  \"Keep crushing your sprint milestones!\"\n", colorGold, colorReset)
	fmt.Printf("%s       `-´       %s  Workspace Status: %s%d Completed%s • %s%d In Flight%s\n", colorGold, colorReset, colorGreen, done, colorReset, colorYellow, inProg, colorReset)
	fmt.Printf("%s      /   \\      %s  Tip: Use 'sunray draw-kanban' or '/draw-gantt' for terminal charts!\n", colorGold, colorReset)
	fmt.Println()
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func longestCommonPrefix(strs []string) string {
	if len(strs) == 0 {
		return ""
	}
	prefix := strs[0]
	for _, s := range strs[1:] {
		for !strings.HasPrefix(s, prefix) {
			if len(prefix) == 0 {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func executeREPLCommand(cfg *Config, spaceID *string, input string) {
	if input == "help" {
		printHelp()
		return
	}

	// Interactive '@' task query/preview trigger
	if strings.HasPrefix(input, "@") {
		if *spaceID == "" {
			fmt.Printf("%sPlease select a space first with:%s spaces switch <space_id>\n", colorYellow, colorReset)
			return
		}
		tasks, err := fetchSpaceTasks(*cfg, *spaceID)
		if err != nil {
			fmt.Printf("%sError fetching tasks:%s %v\n", colorRed, colorReset, err)
			return
		}
		matches := filterTasksByQuery(input, tasks)
		if len(matches) == 0 {
			fmt.Printf("%sNo tasks matching '%s'%s\n", colorGray, input, colorReset)
		} else {
			fmt.Printf("\n%sMatching Tasks (%d found):%s\n", colorGold, len(matches), colorReset)
			for _, m := range matches {
				renderTaskPreviewBox(m)
			}
		}
		return
	}

	args := strings.Fields(input)
	cmd := strings.TrimPrefix(args[0], "/")

	switch cmd {
	case "spaces":
		if len(args) > 1 && args[1] == "list" {
			handleSpacesList(*cfg, false)
		} else if len(args) > 1 && args[1] == "switch" {
			query := ""
			if len(args) > 2 {
				query = strings.Join(args[2:], " ")
			}
			handleSpacesSwitch(cfg, query)
			*spaceID = cfg.SpaceID
		} else {
			fmt.Println("Usage: spaces [list|switch <@name or ID>]")
		}
	case "tasks":
		if len(args) > 1 && args[1] == "list" {
			handleTasksList(*cfg, *spaceID, false)
		} else if len(args) > 2 && args[1] == "create" {
			name := strings.Join(args[2:], " ")
			handleTasksCreate(*cfg, *spaceID, name, 1, "")
		} else {
			fmt.Println("Usage: tasks [list|create <name>]")
		}
	case "draw-kanban", "kanban":
		scope := ""
		if len(args) > 1 {
			scope = args[1]
		}
		handleDrawKanban(*cfg, *spaceID, scope)
	case "draw-gantt", "gantt":
		scope := ""
		if len(args) > 1 {
			scope = args[1]
		}
		handleDrawGantt(*cfg, *spaceID, scope)
	case "draw-tree", "tree":
		scope := ""
		if len(args) > 1 {
			scope = args[1]
		}
		handleDrawTree(*cfg, *spaceID, scope)
	case "sunny":
		handleSunny(*cfg, *spaceID)
	case "evm":
		handleEVM(*cfg, *spaceID, false)
	case "report":
		handleReport(*cfg, *spaceID)
	case "auth":
		if len(args) > 1 && args[1] == "logout" {
			handleAuthLogout()
		} else if len(args) > 1 && args[1] == "status" {
			handleAuthStatus(*cfg)
		} else {
			handleAuthLogin(*cfg)
		}
	case "login":
		handleAuthLogin(*cfg)
	case "logout":
		handleAuthLogout()
	case "status":
		handleAuthStatus(*cfg)
	case "update", "upgrade":
		handleUpdate()
	default:
		fmt.Printf("Unknown command '%s'. Type 'help' for usage.\n", cmd)
	}
}

func startFallbackREPL(cfg Config) {
	scanner := bufio.NewScanner(os.Stdin)
	spaceID := cfg.SpaceID
	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" || input == "exit" || input == "quit" {
			break
		}
		executeREPLCommand(&cfg, &spaceID, input)
	}
}

// Interactive REPL Shell with native terminal raw mode, history, and tab completion
func startInteractiveREPL(cfg Config) {
	fmt.Printf("\n%s[sunRayPM] Interactive REPL Shell%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("Connected to: %s%s%s\n", colorCyan, cfg.BaseURL, colorReset)
	if cfg.SpaceID != "" {
		fmt.Printf("Active Space: %s%s%s\n", colorGold, cfg.SpaceID, colorReset)
	}
	fmt.Println("Type 'help' for commands, [Tab] to autocomplete, '@' to preview tasks, or 'exit' to quit.")
	fmt.Println()

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		startFallbackREPL(cfg)
		return
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		startFallbackREPL(cfg)
		return
	}
	defer term.Restore(fd, oldState)

	screen := struct {
		io.Reader
		io.Writer
	}{os.Stdin, os.Stdout}

	prompt := fmt.Sprintf("%ssunray%s%s❯%s ", colorGold, colorReset, colorCyan, colorReset)
	terminal := term.NewTerminal(screen, prompt)

	commands := []string{
		"spaces list", "spaces switch",
		"tasks list", "tasks create", "tasks update", "tasks delete",
		"draw-kanban", "draw-gantt", "draw-tree",
		"sunny", "evm", "report",
		"auth login", "auth logout", "auth status",
		"login", "logout", "status",
		"update", "upgrade",
		"help", "exit", "quit",
	}

	spaceID := cfg.SpaceID

	terminal.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key == '\t' {
			trimmed := strings.TrimSpace(line[:pos])
			if trimmed == "" {
				return line, pos, false
			}

			// If autocompleting spaces switch
			if strings.HasPrefix(trimmed, "spaces switch") {
				query := strings.TrimSpace(strings.TrimPrefix(trimmed, "spaces switch"))
				spaces, _ := fetchAllSpaces(cfg)
				var matches []string
				for _, s := range spaces {
					if strings.HasPrefix(strings.ToLower(s.Name), strings.ToLower(query)) || strings.HasPrefix(strings.ToLower(s.ID), strings.ToLower(query)) {
						matches = append(matches, "spaces switch "+s.Name)
					}
				}
				if len(matches) == 1 {
					return matches[0] + " ", len(matches[0]) + 1, true
				} else if len(matches) > 1 {
					fmt.Fprintf(terminal, "\r\nAvailable workspaces:\r\n")
					for _, m := range matches {
						fmt.Fprintf(terminal, "  %s\r\n", m)
					}
					return line, pos, false
				}
			}

			// If completing task ref '@'
			if strings.Contains(trimmed, "@") {
				atIdx := strings.LastIndex(trimmed, "@")
				taskQuery := trimmed[atIdx:]
				if spaceID != "" {
					tasks, _ := fetchSpaceTasks(cfg, spaceID)
					var matches []string
					for _, task := range tasks {
						shortID := task.ID
						if len(shortID) > 8 {
							shortID = shortID[:8]
						}
						refID := "@" + shortID
						if strings.HasPrefix(strings.ToLower(refID), strings.ToLower(taskQuery)) {
							matches = append(matches, refID)
						}
					}
					if len(matches) == 1 {
						prefix := trimmed[:atIdx]
						res := prefix + matches[0] + " "
						return res, len(res), true
					}
				}
			}

			var matches []string
			for _, c := range commands {
				if strings.HasPrefix(c, trimmed) {
					matches = append(matches, c)
				}
			}

			if len(matches) == 1 {
				return matches[0] + " ", len(matches[0]) + 1, true
			} else if len(matches) > 1 {
				lcp := longestCommonPrefix(matches)
				if len(lcp) > len(trimmed) {
					return lcp, len(lcp), true
				}
				fmt.Fprintf(terminal, "\r\nAvailable commands:\r\n  %s\r\n", strings.Join(matches, "  "))
				return line, pos, false
			}
		}
		return line, pos, false
	}

	for {
		input, err := terminal.ReadLine()
		if err != nil {
			// EOF or Ctrl+D
			break
		}
		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		if input == "exit" || input == "quit" {
			term.Restore(fd, oldState)
			fmt.Println("Goodbye!")
			break
		}

		// Temporarily restore terminal for standard command execution & output
		term.Restore(fd, oldState)

		executeREPLCommand(&cfg, &spaceID, input)

		// Re-enter raw terminal mode for next input line
		oldState, _ = term.MakeRaw(fd)
	}
}

func main() {
	cfg := getConfig()

	if len(os.Args) < 2 {
		// Default to interactive REPL if no command arguments provided
		startInteractiveREPL(cfg)
		return
	}

	rawCmd := os.Args[1]
	cmd := strings.TrimPrefix(rawCmd, "/") // Support slash commands like /draw-kanban

	switch cmd {
	case "repl", "interactive", "i":
		startInteractiveREPL(cfg)

	case "version", "-v", "--version":
		fmt.Println("sunRay CLI v0.1.0-developer-preview")

	case "help", "--help", "-h":
		printHelp()

	case "spaces":
		if len(os.Args) > 2 && os.Args[2] == "list" {
			jsonOut := false
			for _, a := range os.Args[3:] {
				if a == "--json" {
					jsonOut = true
				}
			}
			handleSpacesList(cfg, jsonOut)
		} else if len(os.Args) > 2 && os.Args[2] == "switch" {
			query := ""
			if len(os.Args) > 3 {
				query = strings.Join(os.Args[3:], " ")
			}
			handleSpacesSwitch(&cfg, query)
		} else {
			fmt.Println("Usage: sunray spaces [list|switch <@name or ID>]")
		}

	case "tasks":
		if len(os.Args) < 3 {
			fmt.Println("Usage: sunray tasks [list|create|update|delete]")
			return
		}
		subCmd := os.Args[2]
		taskFlags := flag.NewFlagSet("tasks", flag.ExitOnError)
		spaceID := taskFlags.String("space-id", "", "Space ID")
		taskRef := taskFlags.String("id", "", "Container / Task Ref (@task or ID)")
		name := taskFlags.String("name", "", "Task Title / Name")
		duration := taskFlags.Int("duration", 1, "Duration in days")
		progress := taskFlags.Int("progress", -1, "Progress percentage (0-100)")
		parentRef := taskFlags.String("parent", "", "Parent Container Ref (@parent)")
		jsonOut := taskFlags.Bool("json", false, "Output raw JSON")
		taskFlags.Parse(os.Args[3:])

		switch subCmd {
		case "list":
			handleTasksList(cfg, *spaceID, *jsonOut)
		case "create":
			handleTasksCreate(cfg, *spaceID, *name, *duration, *parentRef)
		case "update":
			handleTasksUpdate(cfg, *spaceID, *taskRef, *name, *progress, *duration)
		case "delete":
			handleTasksDelete(cfg, *spaceID, *taskRef)
		default:
			fmt.Printf("Unknown tasks subcommand: %s\n", subCmd)
		}

	case "draw-kanban", "kanban":
		kanbanFlags := flag.NewFlagSet("draw-kanban", flag.ExitOnError)
		spaceID := kanbanFlags.String("space-id", "", "Space ID")
		kanbanFlags.Parse(os.Args[2:])
		containerRef := ""
		if len(kanbanFlags.Args()) > 0 {
			containerRef = kanbanFlags.Args()[0]
		}
		handleDrawKanban(cfg, *spaceID, containerRef)

	case "draw-gantt", "gantt":
		ganttFlags := flag.NewFlagSet("draw-gantt", flag.ExitOnError)
		spaceID := ganttFlags.String("space-id", "", "Space ID")
		ganttFlags.Parse(os.Args[2:])
		containerRef := ""
		if len(ganttFlags.Args()) > 0 {
			containerRef = ganttFlags.Args()[0]
		}
		handleDrawGantt(cfg, *spaceID, containerRef)

	case "draw-tree", "tree":
		treeFlags := flag.NewFlagSet("draw-tree", flag.ExitOnError)
		spaceID := treeFlags.String("space-id", "", "Space ID")
		treeFlags.Parse(os.Args[2:])
		containerRef := ""
		if len(treeFlags.Args()) > 0 {
			containerRef = treeFlags.Args()[0]
		}
		handleDrawTree(cfg, *spaceID, containerRef)

	case "sunny":
		sunnyFlags := flag.NewFlagSet("sunny", flag.ExitOnError)
		spaceID := sunnyFlags.String("space-id", "", "Space ID")
		sunnyFlags.Parse(os.Args[2:])
		handleSunny(cfg, *spaceID)

	case "deps":
		if len(os.Args) < 3 {
			fmt.Println("Usage: sunray deps [add]")
			return
		}
		subCmd := os.Args[2]
		depFlags := flag.NewFlagSet("deps", flag.ExitOnError)
		spaceID := depFlags.String("space-id", "", "Space ID")
		pred := depFlags.String("pred", "", "Predecessor Task Ref (@pred)")
		succ := depFlags.String("succ", "", "Successor Task Ref (@succ)")
		depFlags.Parse(os.Args[3:])

		if subCmd == "add" {
			handleDepsAdd(cfg, *spaceID, *pred, *succ)
		} else {
			fmt.Printf("Unknown deps subcommand: %s\n", subCmd)
		}

	case "evm":
		evmFlags := flag.NewFlagSet("evm", flag.ExitOnError)
		spaceID := evmFlags.String("space-id", "", "Space ID")
		jsonOut := evmFlags.Bool("json", false, "Output raw JSON")
		evmFlags.Parse(os.Args[2:])
		handleEVM(cfg, *spaceID, *jsonOut)

	case "auth":
		if len(os.Args) < 3 {
			fmt.Println("Usage: sunray auth [login|logout|status]")
			return
		}
		subCmd := os.Args[2]
		switch subCmd {
		case "login":
			handleAuthLogin(cfg)
		case "logout":
			handleAuthLogout()
		case "status":
			handleAuthStatus(cfg)
		default:
			fmt.Printf("Unknown auth subcommand: %s (use login, logout, status)\n", subCmd)
		}

	case "login":
		handleAuthLogin(cfg)

	case "logout":
		handleAuthLogout()

	case "status":
		handleAuthStatus(cfg)

	case "report":
		reportFlags := flag.NewFlagSet("report", flag.ExitOnError)
		spaceID := reportFlags.String("space-id", "", "Space ID")
		reportFlags.Parse(os.Args[2:])
		handleReport(cfg, *spaceID)

	case "update", "upgrade":
		handleUpdate()

	default:
		fmt.Printf("Unknown command: %s\n\n", rawCmd)
		printHelp()
	}
}

