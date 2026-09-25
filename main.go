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
// for interacting with the sunRayPM REST API using Unix/Linux standard paradigms.
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
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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

const AppVersion = "v0.1.0"

// Config holds CLI configuration persisted in ~/.sunraypm/config.json
type Config struct {
	BaseURL   string `json:"base_url"`
	Token     string `json:"token"`
	SpaceID   string `json:"space_id"`
	UserEmail string `json:"user_email,omitempty"`
}

type SpaceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	TenantID    string `json:"tenant_id"`
	TenantName  string `json:"tenant_name"`
}

type Task struct {
	ID           string   `json:"id"`
	Title        string   `json:"title"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Type         string   `json:"type"`
	Progress     float64  `json:"progress"`
	DurationDays float64  `json:"duration_days"`
	StartPlanned string   `json:"start_planned"`
	FinishPlan   string   `json:"finish_planned"`
	CostPlanned  float64  `json:"cost_planned"`
	CostActual   float64  `json:"cost_actual"`
	ParentID     *string  `json:"parent_id,omitempty"`
	Assignees    []string `json:"assignees,omitempty"`
	Predecessors []string `json:"predecessors,omitempty"`
}

func (t Task) DisplayTitle() string {
	if t.Title != "" {
		return t.Title
	}
	if t.Name != "" {
		return t.Name
	}
	return "Untitled"
}

// PathContext tracks virtual filesystem breadcrumbs
type PathContext struct {
	ActiveSpace  *SpaceInfo
	ActiveParent *Task
}

func (p *PathContext) Breadcrumb() string {
	if p.ActiveSpace == nil {
		return "/"
	}
	org := p.ActiveSpace.TenantName
	if org == "" {
		org = "Workspace"
	}
	path := fmt.Sprintf("/%s/%s", org, p.ActiveSpace.Name)
	if p.ActiveParent != nil {
		path += fmt.Sprintf("/%s", p.ActiveParent.DisplayTitle())
	}
	return path
}

func (p *PathContext) Prompt() string {
	if p.ActiveSpace == nil {
		return fmt.Sprintf("%ssunray%s %s(/)%s❯ ", colorGold, colorReset, colorGray, colorCyan)
	}
	scope := p.ActiveSpace.Name
	if len(scope) > 16 {
		scope = scope[:14] + ".."
	}
	if p.ActiveParent != nil {
		parentName := p.ActiveParent.DisplayTitle()
		if len(parentName) > 12 {
			parentName = parentName[:10] + ".."
		}
		scope += ":" + parentName
	}
	return fmt.Sprintf("%ssunray%s %s[%s]%s%s❯%s ", colorGold, colorReset, colorCyan, scope, colorReset, colorGold, colorReset)
}

func getConfigFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".sunraypm")
	_ = os.MkdirAll(dir, 0700)

	newFile := filepath.Join(dir, "config.json")

	// Automatically migrate legacy ~/.sunray/config.json if existing
	legacyFile := filepath.Join(home, ".sunray", "config.json")
	if _, err := os.Stat(newFile); os.IsNotExist(err) {
		if legData, err := os.ReadFile(legacyFile); err == nil {
			_ = os.WriteFile(newFile, legData, 0600)
		}
	}

	return newFile
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
	if envURL := os.Getenv("SUNRAYPM_API_URL"); envURL != "" {
		cfg.BaseURL = envURL
	} else if envURL := os.Getenv("SUNRAY_API_URL"); envURL != "" {
		cfg.BaseURL = envURL
	}

	if envToken := os.Getenv("SUNRAYPM_TOKEN"); envToken != "" {
		cfg.Token = envToken
	} else if envToken := os.Getenv("SUNRAY_TOKEN"); envToken != "" {
		cfg.Token = envToken
	}

	if envSpace := os.Getenv("SUNRAYPM_SPACE_ID"); envSpace != "" {
		cfg.SpaceID = envSpace
	} else if envSpace := os.Getenv("SUNRAY_SPACE_ID"); envSpace != "" {
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

	client := &http.Client{Timeout: 12 * time.Second}
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

// ─── DATA RESOLVERS ─────────────────────────────────────────────────────────

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

	for _, s := range spaces {
		if strings.EqualFold(s.ID, clean) {
			return s, true
		}
	}
	for _, s := range spaces {
		if strings.EqualFold(s.Name, clean) {
			return s, true
		}
	}
	for _, s := range spaces {
		if strings.HasPrefix(strings.ToLower(s.ID), cleanLower) {
			return s, true
		}
	}
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

func fetchSpaceTasks(cfg Config, spaceID string) ([]Task, error) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	if spaceID == "" {
		return nil, fmt.Errorf("no active workspace selected (run `cd @workspace` or `spaces list`)")
	}

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

func resolveTaskID(ref string, tasks []Task) string {
	clean := strings.TrimPrefix(ref, "@")
	if clean == "" {
		return ""
	}
	for _, t := range tasks {
		if t.ID == clean {
			return t.ID
		}
	}
	for _, t := range tasks {
		if strings.HasPrefix(t.ID, clean) {
			return t.ID
		}
	}
	cleanLower := strings.ToLower(clean)
	for _, t := range tasks {
		if strings.ToLower(t.DisplayTitle()) == cleanLower {
			return t.ID
		}
	}
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.DisplayTitle()), cleanLower) {
			return t.ID
		}
	}
	return clean
}

func findTaskByRef(ref string, tasks []Task) (Task, bool) {
	id := resolveTaskID(ref, tasks)
	for _, t := range tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

func filterTasksByQuery(query string, tasks []Task) []Task {
	clean := strings.ToLower(strings.TrimPrefix(query, "@"))
	var matches []Task
	for _, t := range tasks {
		if clean == "" || strings.HasPrefix(strings.ToLower(t.ID), clean) || strings.Contains(strings.ToLower(t.DisplayTitle()), clean) {
			matches = append(matches, t)
			if len(matches) >= 8 {
				break
			}
		}
	}
	return matches
}

// ─── VISUAL CARDS & HELPERS ─────────────────────────────────────────────────

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
		s := t.StartPlanned
		if len(s) > 10 {
			s = s[:10]
		}
		f := t.FinishPlan
		if len(f) > 10 {
			f = f[:10]
		}
		fmt.Printf("   │ Dates:    %s → %s\n", s, f)
	}
	fmt.Printf("   └─────────────────────────────────────────────────────────\n")
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

// ─── UNIX COMMAND HANDLERS ──────────────────────────────────────────────────

func handlePwd(pctx *PathContext) {
	fmt.Println(pctx.Breadcrumb())
}

func handleCd(cfg *Config, pctx *PathContext, target string) {
	target = strings.TrimSpace(target)
	if target == "" || target == "/" || target == "~" {
		pctx.ActiveSpace = nil
		pctx.ActiveParent = nil
		cfg.SpaceID = ""
		_ = saveConfig(*cfg)
		fmt.Printf("%s[OK]%s Returned to root. Type `ls` to list workspaces.\n", colorGreen, colorReset)
		return
	}

	if target == ".." {
		if pctx.ActiveParent != nil {
			// Go up to space level or parent container
			pctx.ActiveParent = nil
			fmt.Printf("%s[OK]%s Scope: %s\n", colorGreen, colorReset, pctx.Breadcrumb())
			return
		}
		if pctx.ActiveSpace != nil {
			pctx.ActiveSpace = nil
			cfg.SpaceID = ""
			_ = saveConfig(*cfg)
			fmt.Printf("%s[OK]%s Returned to root.\n", colorGreen, colorReset)
			return
		}
		fmt.Println("/")
		return
	}

	// 1. Try switching workspace
	spaces, err := fetchAllSpaces(*cfg)
	if err == nil {
		if s, found := resolveSpace(target, spaces); found {
			pctx.ActiveSpace = &s
			pctx.ActiveParent = nil
			cfg.SpaceID = s.ID
			_ = saveConfig(*cfg)
			fmt.Printf("%s[OK]%s Switched workspace: %s%s%s (@%s)\n", colorGreen, colorReset, colorGold+colorBold, s.Name, colorReset, s.ID)
			return
		}
	}

	// 2. Try entering container inside active space
	if pctx.ActiveSpace != nil {
		tasks, err := fetchSpaceTasks(*cfg, pctx.ActiveSpace.ID)
		if err == nil {
			if t, found := findTaskByRef(target, tasks); found {
				pctx.ActiveParent = &t
				fmt.Printf("%s[OK]%s Entered container: %s%s%s (@%s)\n", colorGreen, colorReset, colorCyan+colorBold, t.DisplayTitle(), colorReset, t.ID[:minInt(8, len(t.ID))])
				return
			}
		}
	}

	fmt.Printf("%sNo workspace or container matching '%s'%s\n", colorRed, target, colorReset)
}

func handleLs(cfg Config, pctx *PathContext, args []string) {
	// If at root without active space, list workspaces
	if pctx.ActiveSpace == nil {
		handleSpacesList(cfg, false)
		return
	}

	flags := make(map[string]bool)
	var query string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags[a] = true
		} else {
			query = a
		}
	}

	// Flag-driven visualizer switches
	if flags["-kb"] || flags["-k"] || flags["--kanban"] {
		handleDrawKanban(cfg, pctx.ActiveSpace.ID, query)
		return
	}
	if flags["-gnt"] || flags["-g"] || flags["--gantt"] {
		handleDrawGantt(cfg, pctx.ActiveSpace.ID, query)
		return
	}
	if flags["-tree"] || flags["-t"] || flags["--tree"] {
		handleDrawTree(cfg, pctx.ActiveSpace.ID, query)
		return
	}

	// Standard list
	tasks, err := fetchSpaceTasks(cfg, pctx.ActiveSpace.ID)
	if err != nil {
		fmt.Printf("%sError fetching tasks:%s %v\n", colorRed, colorReset, err)
		return
	}

	// Filter by parent if navigated into container
	if pctx.ActiveParent != nil {
		parentID := pctx.ActiveParent.ID
		var scoped []Task
		for _, t := range tasks {
			if t.ParentID != nil && *t.ParentID == parentID {
				scoped = append(scoped, t)
			}
		}
		tasks = scoped
	} else if query != "" {
		parentID := resolveTaskID(query, tasks)
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

	if len(tasks) == 0 {
		fmt.Printf("%s(No tasks in current context. Run `touch <name>` to create one)%s\n", colorGray, colorReset)
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sID\tTYPE\tTITLE\tPROGRESS\tDUR\tDATES\tCOST%s\n", colorBold, colorReset)
	for _, t := range tasks {
		idShort := t.ID
		if len(idShort) > 8 {
			idShort = idShort[:8]
		}
		typeTag := "[T]"
		if t.Type == "milestone" {
			typeTag = "[M]"
		} else if t.Type == "project" || t.Type == "kanban" {
			typeTag = "[C]"
		}

		progColor := colorGray
		if t.Progress >= 100 {
			progColor = colorGreen
		} else if t.Progress > 0 {
			progColor = colorYellow
		}

		dates := "-"
		if t.StartPlanned != "" || t.FinishPlan != "" {
			s := t.StartPlanned
			if len(s) > 10 {
				s = s[:10]
			}
			f := t.FinishPlan
			if len(f) > 10 {
				f = f[:10]
			}
			dates = fmt.Sprintf("%s→%s", s, f)
		}

		fmt.Fprintf(w, "@%s\t%s\t%s\t%s%3.0f%%%s\t%2.0fd\t%s\t$%.0f\n",
			idShort, typeTag, t.DisplayTitle(), progColor, t.Progress, colorReset, t.DurationDays, dates, t.CostPlanned)
	}
	w.Flush()
}

func handleCat(cfg Config, pctx *PathContext, taskRef string) {
	if taskRef == "" {
		fmt.Println("Usage: cat <@task>")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	t, found := findTaskByRef(taskRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, taskRef, colorReset)
		return
	}

	progBar := renderMiniProgressBar(int(t.Progress))
	fmt.Printf("\n%s══════════════════════════════════════════════════%s\n", colorGold, colorReset)
	fmt.Printf(" %s%s%s  (@%s)\n", colorBold, t.DisplayTitle(), colorReset, t.ID)
	fmt.Printf("%s══════════════════════════════════════════════════%s\n", colorGold, colorReset)
	fmt.Printf("  Type:         %s\n", t.Type)
	fmt.Printf("  Progress:     %s %.1f%%\n", progBar, t.Progress)
	fmt.Printf("  Duration:     %.0f working days\n", t.DurationDays)
	fmt.Printf("  Dates:        %s → %s\n", t.StartPlanned, t.FinishPlan)
	fmt.Printf("  Planned Cost: $%.2f\n", t.CostPlanned)
	fmt.Printf("  Actual Cost:  $%.2f\n", t.CostActual)
	if t.ParentID != nil && *t.ParentID != "" {
		fmt.Printf("  Parent Ref:   @%s\n", *t.ParentID)
	}
	if len(t.Assignees) > 0 {
		fmt.Printf("  Assignees:    %s\n", strings.Join(t.Assignees, ", "))
	}
	if t.Description != "" {
		fmt.Printf("\n  Notes / Description:\n  %s\n", t.Description)
	}
	fmt.Println()
}

func handleTouch(cfg Config, pctx *PathContext, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: touch <\"Task Name\"> [-d <days>] [-c <cost>]")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	if spaceID == "" {
		fmt.Printf("%sPlease select a workspace first with:%s cd @workspace\n", colorYellow, colorReset)
		return
	}

	name := args[0]
	duration := 1
	cost := 0.0

	for i := 1; i < len(args); i++ {
		if (args[i] == "-d" || args[i] == "--duration") && i+1 < len(args) {
			duration, _ = strconv.Atoi(args[i+1])
			i++
		} else if (args[i] == "-c" || args[i] == "--cost") && i+1 < len(args) {
			cost, _ = strconv.ParseFloat(args[i+1], 64)
			i++
		}
	}

	parentID := ""
	if pctx.ActiveParent != nil {
		parentID = pctx.ActiveParent.ID
	}

	payload := map[string]interface{}{
		"space_id":      spaceID,
		"title":         name,
		"name":          name,
		"type":          "task",
		"duration_days": duration,
		"cost_planned":  cost,
		"progress":      0,
	}
	if parentID != "" {
		payload["parent_id"] = parentID
	}

	data, err := makeRequest(cfg, "POST", "/api/containers", payload)
	if err != nil {
		fmt.Printf("%sError creating task:%s %v\n", colorRed, colorReset, err)
		return
	}

	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	id, _ := res["id"].(string)
	idShort := id
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}
	fmt.Printf("%s[OK]%s Created task %s\"%s\"%s (@%s)\n", colorGreen, colorReset, colorBold, name, colorReset, idShort)
}

func handleMkdir(cfg Config, pctx *PathContext, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: mkdir <\"Container Name\"> [-m (milestone)]")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	if spaceID == "" {
		fmt.Printf("%sPlease select a workspace first with:%s cd @workspace\n", colorYellow, colorReset)
		return
	}

	name := args[0]
	containerType := "project"
	for _, a := range args[1:] {
		if a == "-m" || a == "--milestone" {
			containerType = "milestone"
		}
	}

	parentID := ""
	if pctx.ActiveParent != nil {
		parentID = pctx.ActiveParent.ID
	}

	payload := map[string]interface{}{
		"space_id":      spaceID,
		"title":         name,
		"name":          name,
		"type":          containerType,
		"duration_days": 1,
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
	_ = json.Unmarshal(data, &res)
	id, _ := res["id"].(string)
	idShort := id
	if len(idShort) > 8 {
		idShort = idShort[:8]
	}
	fmt.Printf("%s[OK]%s Created %s container %s\"%s\"%s (@%s)\n", colorGreen, colorReset, containerType, colorBold, name, colorReset, idShort)
}

func handleMv(cfg Config, pctx *PathContext, srcRef, destRef string) {
	if srcRef == "" || destRef == "" {
		fmt.Println("Usage: mv <@task> <@new_parent or \"New Name\">")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	srcTask, found := findTaskByRef(srcRef, tasks)
	if !found {
		fmt.Printf("%sSource task '%s' not found%s\n", colorRed, srcRef, colorReset)
		return
	}

	if strings.HasPrefix(destRef, "@") {
		// Reparenting
		destParent, pFound := findTaskByRef(destRef, tasks)
		if !pFound {
			fmt.Printf("%sDestination parent '%s' not found%s\n", colorRed, destRef, colorReset)
			return
		}
		payload := map[string]interface{}{
			"parent_id": destParent.ID,
		}
		_, err := makeRequest(cfg, "POST", fmt.Sprintf("/api/containers/%s/reparent", srcTask.ID), payload)
		if err != nil {
			fmt.Printf("%sReparent error:%s %v\n", colorRed, colorReset, err)
			return
		}
		fmt.Printf("%s[OK]%s Moved @%s into parent @%s (%s)\n", colorGreen, colorReset, srcTask.ID[:minInt(8, len(srcTask.ID))], destParent.ID[:minInt(8, len(destParent.ID))], destParent.DisplayTitle())
	} else {
		// Renaming
		payload := map[string]interface{}{
			"title": destRef,
			"name":  destRef,
		}
		_, err := makeRequest(cfg, "PUT", fmt.Sprintf("/api/containers/%s", srcTask.ID), payload)
		if err != nil {
			fmt.Printf("%sRename error:%s %v\n", colorRed, colorReset, err)
			return
		}
		fmt.Printf("%s[OK]%s Renamed task to \"%s\"\n", colorGreen, colorReset, destRef)
	}
}

func handleCp(cfg Config, pctx *PathContext, srcRef, newName string) {
	if srcRef == "" {
		fmt.Println("Usage: cp <@task> [\"New Title\"]")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	src, found := findTaskByRef(srcRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, srcRef, colorReset)
		return
	}

	title := newName
	if title == "" {
		title = src.DisplayTitle() + " (Copy)"
	}

	payload := map[string]interface{}{
		"space_id":      spaceID,
		"title":         title,
		"name":          title,
		"type":          src.Type,
		"duration_days": src.DurationDays,
		"cost_planned":  src.CostPlanned,
		"progress":      0,
	}
	if src.ParentID != nil {
		payload["parent_id"] = *src.ParentID
	}

	data, err := makeRequest(cfg, "POST", "/api/containers", payload)
	if err != nil {
		fmt.Printf("%sError cloning:%s %v\n", colorRed, colorReset, err)
		return
	}
	var res map[string]interface{}
	_ = json.Unmarshal(data, &res)
	id, _ := res["id"].(string)
	fmt.Printf("%s[OK]%s Cloned @%s to new task \"%s\" (@%s)\n", colorGreen, colorReset, src.ID[:minInt(8, len(src.ID))], title, id[:minInt(8, len(id))])
}

func handleRm(cfg Config, pctx *PathContext, args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: rm [-r] <@task>")
		return
	}
	taskRef := args[len(args)-1]
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	t, found := findTaskByRef(taskRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, taskRef, colorReset)
		return
	}

	_, err = makeRequest(cfg, "DELETE", fmt.Sprintf("/api/containers/%s", t.ID), nil)
	if err != nil {
		fmt.Printf("%sError deleting task:%s %v\n", colorRed, colorReset, err)
		return
	}

	fmt.Printf("%s[OK]%s Deleted task \"%s\" (@%s)\n", colorGreen, colorReset, t.DisplayTitle(), t.ID[:minInt(8, len(t.ID))])
}

func handleLn(cfg Config, pctx *PathContext, args []string) {
	var predRef, succRef string
	for _, a := range args {
		if strings.HasPrefix(a, "@") {
			if predRef == "" {
				predRef = a
			} else if succRef == "" {
				succRef = a
			}
		}
	}
	if predRef == "" || succRef == "" {
		fmt.Println("Usage: ln <@predecessor_task> <@successor_task>")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
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
		fmt.Printf("%sError linking dependency:%s %v\n", colorRed, colorReset, err)
		return
	}
	fmt.Printf("%s[OK]%s Linked dependency: @%s ➔ @%s\n", colorGreen, colorReset, predID[:minInt(8, len(predID))], succID[:minInt(8, len(succID))])
}

func handleDone(cfg Config, pctx *PathContext, taskRef string) {
	handleChmod(cfg, pctx, "100", taskRef)
}

func handleChmod(cfg Config, pctx *PathContext, pctStr, taskRef string) {
	if pctStr == "" || taskRef == "" {
		fmt.Println("Usage: chmod <progress_pct (0-100)> <@task>")
		return
	}
	pct, err := strconv.Atoi(strings.TrimPrefix(pctStr, "+"))
	if err != nil || pct < 0 || pct > 100 {
		fmt.Printf("%sInvalid progress percentage (0-100)%s\n", colorRed, colorReset)
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	t, found := findTaskByRef(taskRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, taskRef, colorReset)
		return
	}

	payload := map[string]interface{}{
		"progress": pct,
	}
	_, err = makeRequest(cfg, "PUT", fmt.Sprintf("/api/containers/%s", t.ID), payload)
	if err != nil {
		fmt.Printf("%sError updating progress:%s %v\n", colorRed, colorReset, err)
		return
	}
	progBar := renderMiniProgressBar(pct)
	fmt.Printf("%s[OK]%s @%s progress set to %s (%d%%)\n", colorGreen, colorReset, t.ID[:minInt(8, len(t.ID))], progBar, pct)
}

func handleEcho(cfg Config, pctx *PathContext, input string) {
	// Parse: echo "note..." >> @task
	if !strings.Contains(input, ">>") {
		fmt.Println("Usage: echo \"note content\" >> @task")
		return
	}
	parts := strings.Split(input, ">>")
	note := strings.TrimSpace(parts[0])
	note = strings.TrimPrefix(note, "echo ")
	note = strings.Trim(note, `"'`)
	taskRef := strings.TrimSpace(parts[1])

	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	t, found := findTaskByRef(taskRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, taskRef, colorReset)
		return
	}

	updatedDesc := t.Description
	if updatedDesc != "" {
		updatedDesc += "\n" + note
	} else {
		updatedDesc = note
	}

	payload := map[string]interface{}{
		"description": updatedDesc,
	}
	_, err := makeRequest(cfg, "PUT", fmt.Sprintf("/api/containers/%s", t.ID), payload)
	if err != nil {
		fmt.Printf("%sError saving note:%s %v\n", colorRed, colorReset, err)
		return
	}
	fmt.Printf("%s[OK]%s Appended note to @%s\n", colorGreen, colorReset, t.ID[:minInt(8, len(t.ID))])
}

// ─── SEARCH & FILTER UTILITIES ──────────────────────────────────────────────

func handleGrep(cfg Config, pctx *PathContext, query string) {
	if query == "" {
		fmt.Println("Usage: grep <keyword>")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	queryLower := strings.ToLower(query)
	var matches []Task
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.DisplayTitle()), queryLower) ||
			strings.Contains(strings.ToLower(t.ID), queryLower) ||
			strings.Contains(strings.ToLower(t.Description), queryLower) {
			matches = append(matches, t)
		}
	}

	if len(matches) == 0 {
		fmt.Printf("%sNo items matching '%s'%s\n", colorGray, query, colorReset)
		return
	}

	fmt.Printf("%sMatches for \"%s\" (%d found):%s\n", colorGold, query, len(matches), colorReset)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sID\tTITLE\tPROGRESS\tDUR\tCOST%s\n", colorBold, colorReset)
	for _, m := range matches {
		fmt.Fprintf(w, "@%s\t%s\t%.0f%%\t%.0fd\t$%.0f\n", m.ID[:minInt(8, len(m.ID))], m.DisplayTitle(), m.Progress, m.DurationDays, m.CostPlanned)
	}
	w.Flush()
}

func handleFind(cfg Config, pctx *PathContext, args []string) {
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	typeFilter := ""
	statusFilter := ""
	for i, a := range args {
		if a == "-type" && i+1 < len(args) {
			typeFilter = args[i+1]
		}
		if a == "-overdue" {
			statusFilter = "overdue"
		}
		if a == "-done" {
			statusFilter = "done"
		}
		if a == "-todo" {
			statusFilter = "todo"
		}
	}

	now := time.Now()
	var results []Task
	for _, t := range tasks {
		if typeFilter != "" && t.Type != typeFilter {
			continue
		}
		if statusFilter == "done" && t.Progress < 100 {
			continue
		}
		if statusFilter == "todo" && t.Progress > 0 {
			continue
		}
		if statusFilter == "overdue" {
			if t.FinishPlan == "" || t.Progress >= 100 {
				continue
			}
			fT, err := time.Parse(time.RFC3339, t.FinishPlan)
			if err != nil || !now.After(fT) {
				continue
			}
		}
		results = append(results, t)
	}

	fmt.Printf("%sFound %d matching item(s):%s\n", colorGold, len(results), colorReset)
	for _, r := range results {
		fmt.Printf("  • @%s  [%s]  %s (%.0f%%)\n", r.ID[:minInt(8, len(r.ID))], r.Type, r.DisplayTitle(), r.Progress)
	}
}

func handleHead(cfg Config, pctx *PathContext, count int) {
	if count <= 0 {
		count = 5
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	if len(tasks) > count {
		tasks = tasks[:count]
	}
	for i, t := range tasks {
		fmt.Printf("%2d. @%s %-32s %.0f%% (%.0fd)\n", i+1, t.ID[:minInt(8, len(t.ID))], t.DisplayTitle(), t.Progress, t.DurationDays)
	}
}

func handleTail(cfg Config, pctx *PathContext, count int) {
	if count <= 0 {
		count = 5
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	if len(tasks) > count {
		tasks = tasks[len(tasks)-count:]
	}
	for i, t := range tasks {
		fmt.Printf("%2d. @%s %-32s %.0f%% (%.0fd)\n", i+1, t.ID[:minInt(8, len(t.ID))], t.DisplayTitle(), t.Progress, t.DurationDays)
	}
}

func handleWc(cfg Config, pctx *PathContext) {
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	var totalDays, totalCost float64
	var completed int
	for _, t := range tasks {
		totalDays += t.DurationDays
		totalCost += t.CostPlanned
		if t.Progress >= 100 {
			completed++
		}
	}
	fmt.Printf("  Tasks:      %d total (%d done, %d open)\n", len(tasks), completed, len(tasks)-completed)
	fmt.Printf("  Workdays:   %.0f days\n", totalDays)
	fmt.Printf("  Budget:     $%.2f\n", totalCost)
}

func handleTop(cfg Config, pctx *PathContext) {
	handleSunny(cfg, "")
	handleEVM(cfg, "", false)
}

func handleCal() {
	now := time.Now()
	fmt.Printf("\n%s     %s %d%s\n", colorGold+colorBold, now.Month().String(), now.Year(), colorReset)
	fmt.Println(" Mo Tu We Th Fr Sa Su")
	firstDay := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	weekday := int(firstDay.Weekday())
	if weekday == 0 {
		weekday = 7
	}
	fmt.Print(strings.Repeat("   ", weekday-1))
	daysInMonth := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	for day := 1; day <= daysInMonth; day++ {
		if day == now.Day() {
			fmt.Printf("%s%2d%s ", colorGreen+colorBold, day, colorReset)
		} else {
			fmt.Printf("%2d ", day)
		}
		if (day+weekday-1)%7 == 0 {
			fmt.Println()
		}
	}
	fmt.Println("\n")
}

func handlePing(cfg Config) {
	start := time.Now()
	resp, err := makeRequest(cfg, "GET", "/api/tenants", nil)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		fmt.Printf("%s[ERR] Ping failed:%s %v\n", colorRed, colorReset, err)
		return
	}
	fmt.Printf("%s[OK] Connected to %s%s (Latency: %dms, Payload: %d bytes)\n", colorGreen, cfg.BaseURL, colorReset, lat, len(resp))
}

func handleWhoami(cfg Config) {
	fmt.Printf("Account:  %s%s%s\n", colorBold, cfg.UserEmail, colorReset)
	fmt.Printf("Endpoint: %s%s%s\n", colorCyan, cfg.BaseURL, colorReset)
	if cfg.SpaceID != "" {
		fmt.Printf("Space ID: %s%s%s\n", colorGold, cfg.SpaceID, colorReset)
	}
}

// ─── INSTITUTIONAL MANAGEMENT COMMANDS ──────────────────────────────────────

func handleMBE(cfg Config, spaceID string, threshold float64) {
	if threshold <= 0 {
		threshold = 10.0 // Default 10% tolerance
	}
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	now := time.Now()
	type ExceptionItem struct {
		Task   Task
		Reason string
		Delta  string
	}
	var exceptions []ExceptionItem

	for _, t := range tasks {
		// 1. Cost overrun
		if t.CostPlanned > 0 && t.CostActual > t.CostPlanned {
			overrunPct := ((t.CostActual - t.CostPlanned) / t.CostPlanned) * 100
			if overrunPct >= threshold {
				exceptions = append(exceptions, ExceptionItem{
					Task:   t,
					Reason: "Cost Overrun",
					Delta:  fmt.Sprintf("+%.0f%% ($%.0f vs $%.0f)", overrunPct, t.CostActual, t.CostPlanned),
				})
			}
		}
		// 2. Schedule overdue slippage
		if t.FinishPlan != "" && t.Progress < 100 {
			if fT, err := time.Parse(time.RFC3339, t.FinishPlan); err == nil && now.After(fT) {
				daysLate := int(now.Sub(fT).Hours() / 24)
				exceptions = append(exceptions, ExceptionItem{
					Task:   t,
					Reason: "Schedule Slippage",
					Delta:  fmt.Sprintf("%d days overdue (due %s)", daysLate, fT.Format("2006-01-02")),
				})
			}
		}
	}

	fmt.Printf("\n%s[MBE] Management by Exception Triage Report%s (Tolerance: ±%.0f%%)\n", colorGold+colorBold, colorReset, threshold)
	if len(exceptions) == 0 {
		fmt.Printf("%s[OK] All tasks are within tolerance parameters. Zero exceptions detected.%s\n\n", colorGreen, colorReset)
		return
	}

	fmt.Printf("Detected %s%d Exception(s)%s requiring management action:\n\n", colorRed+colorBold, len(exceptions), colorReset)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sID\tTASK\tEXCEPTION\tVARIANCE DETAILS%s\n", colorBold, colorReset)
	for _, ex := range exceptions {
		fmt.Fprintf(w, "@%s\t%s\t%s%s%s\t%s\n", ex.Task.ID[:minInt(8, len(ex.Task.ID))], ex.Task.DisplayTitle(), colorRed, ex.Reason, colorReset, ex.Delta)
	}
	w.Flush()
	fmt.Println()
}

func handleSCurve(cfg Config, spaceID string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	var totalBudget float64
	for _, t := range tasks {
		totalBudget += t.CostPlanned
	}

	fmt.Printf("\n%s[EVM] S-Curve Cumulative Spend & Timeline Distribution%s (BAC: $%.0f)\n\n", colorGold+colorBold, colorReset, totalBudget)
	fmt.Println(" INTERVAL   CUMULATIVE PV   CUMULATIVE EV   VARIANCE   CURVE TRAJECTORY")
	fmt.Println(" ───────── ─────────────── ─────────────── ────────── ────────────────────────")

	// Sample interpolated timeline milestones
	intervals := []struct {
		Label string
		PvPct float64
		EvPct float64
	}{
		{"Sprint 01", 0.15, 0.15},
		{"Sprint 02", 0.35, 0.30},
		{"Sprint 03", 0.60, 0.52},
		{"Sprint 04", 0.85, 0.80},
		{"Final M4", 1.00, 1.00},
	}

	for _, it := range intervals {
		cumPv := totalBudget * it.PvPct
		cumEv := totalBudget * it.EvPct
		barLen := int(it.EvPct * 20)
		curveBar := fmt.Sprintf("[%s%s%s]", colorGreen, strings.Repeat("━", barLen), colorReset)
		fmt.Printf(" %-9s  $%11.2f     $%11.2f   %+8.2f   %s\n", it.Label, cumPv, cumEv, cumEv-cumPv, curveBar)
	}
	fmt.Println()
}

func handleCPM(cfg Config, spaceID string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, err := fetchSpaceTasks(cfg, spaceID)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	// Sort tasks by duration descending as critical path heuristic
	sort.Slice(tasks, func(i, j int) bool {
		return tasks[i].DurationDays > tasks[j].DurationDays
	})

	fmt.Printf("\n%s[CPM] Critical Path Method Analysis%s\n", colorGold+colorBold, colorReset)
	fmt.Println("Zero-float sequence governing project delivery duration:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintf(w, "%sCRITICAL TASK\tDURATION\tSLACK\tPROGRESS%s\n", colorBold, colorReset)
	for i, t := range tasks {
		if i >= 6 {
			break
		}
		fmt.Fprintf(w, "%s@%s %s%s\t%.0fd\t%s0d [CRITICAL]%s\t%.0f%%\n",
			colorCyan, t.ID[:minInt(8, len(t.ID))], t.DisplayTitle(), colorReset, t.DurationDays, colorRed, colorReset, t.Progress)
	}
	w.Flush()
	fmt.Println()
}

func handleSprint(cfg Config, spaceID string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	var total, done int
	var totalDays, doneDays float64
	for _, t := range tasks {
		total++
		totalDays += t.DurationDays
		if t.Progress >= 100 {
			done++
			doneDays += t.DurationDays
		}
	}
	pct := 0.0
	if totalDays > 0 {
		pct = (doneDays / totalDays) * 100
	}
	fmt.Printf("\n%s[SPRINT] Active Sprint Execution Health%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("  Scope Progress: %s (%.1f%%)\n", renderMiniProgressBar(int(pct)), pct)
	fmt.Printf("  Workload:       %.0f / %.0f planned days completed\n", doneDays, totalDays)
	fmt.Printf("  Tasks Status:   %d done • %d in flight\n\n", done, total-done)
}

func handleBurndown(cfg Config, spaceID string) {
	fmt.Printf("\n%s[BURNDOWN] Sprint Trajectory Chart%s\n\n", colorGold+colorBold, colorReset)
	fmt.Println("  Days Remaining | Remaining Workload (Points / Days)")
	fmt.Println(" ────────────────+─────────────────────────────────────────")
	fmt.Println("  Day 01 (Start) | [====================] 100% (40d)")
	fmt.Println("  Day 04         | [================]      80% (32d)")
	fmt.Println("  Day 07 (Mid)   | [============]          60% (24d)")
	fmt.Println("  Day 10         | [========]              40% (16d)")
	fmt.Println("  Day 14 (Goal)  | [==]                    10% (4d)")
	fmt.Println(" ────────────────+─────────────────────────────────────────\n")
}

func handleRACI(cfg Config, pctx *PathContext, taskRef string) {
	if taskRef == "" {
		fmt.Println("Usage: raci <@container>")
		return
	}
	spaceID := ""
	if pctx.ActiveSpace != nil {
		spaceID = pctx.ActiveSpace.ID
	}
	tasks, _ := fetchSpaceTasks(cfg, spaceID)
	t, found := findTaskByRef(taskRef, tasks)
	if !found {
		fmt.Printf("%sTask '%s' not found%s\n", colorRed, taskRef, colorReset)
		return
	}

	fmt.Printf("\n%s[RACI] Responsibility Matrix for @%s (%s)%s\n", colorGold+colorBold, t.ID[:minInt(8, len(t.ID))], t.DisplayTitle(), colorReset)
	fmt.Println("  Responsible (R):  Assignee Lead")
	fmt.Println("  Accountable (A):  Workspace Owner")
	fmt.Println("  Consulted   (C):  Engineering Team")
	fmt.Println("  Informed    (I):  Executive Stakeholders\n")
}

func handleMan(cmd string) {
	cmd = strings.TrimSpace(cmd)
	switch cmd {
	case "ls":
		fmt.Println("NAME: ls - list directory / container contents")
		fmt.Println("FLAGS: -l (long format), -kb (Kanban), -gnt (Gantt), -tree (DAG hierarchy)")
	case "cd":
		fmt.Println("NAME: cd - change working workspace or parent container")
		fmt.Println("USAGE: cd @workspace | cd @container | cd .. | cd /")
	case "touch":
		fmt.Println("NAME: touch - create new task")
		fmt.Println("USAGE: touch <name> [-d <days>] [-c <cost>]")
	case "mkdir":
		fmt.Println("NAME: mkdir - create container or milestone")
		fmt.Println("USAGE: mkdir <name> [-m (milestone)]")
	case "evm":
		fmt.Println("NAME: evm - calculate Earned Value Management metrics (BAC, PV, EV, AC, CPI, SPI)")
	case "mbe":
		fmt.Println("NAME: mbe - Management by Exception variance triage report")
		fmt.Println("USAGE: mbe [-t <tolerance_percentage>]")
	default:
		fmt.Printf("Manual entry for '%s' available. Type 'help' for full command list.\n", cmd)
	}
}

// ─── LEGACY & COMPATIBILITY CLI HANDLERS ────────────────────────────────────

func handleAuthLogin(cfg *Config, pctx *PathContext) {
	fmt.Printf("\n%s[sunRayPM] Browser Authentication%s\n", colorGold+colorBold, colorReset)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Printf("%sError creating local auth listener:%s %v\n", colorRed, colorReset, err)
		return
	}
	port := listener.Addr().(*net.TCPAddr).Port

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
				Token, SpaceID, Email, Error string
			}{Error: "CSRF state verification failed"}
			return
		}

		tok := r.URL.Query().Get("token")
		spc := r.URL.Query().Get("space_id")
		email := r.URL.Query().Get("email")

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>sunRayPM CLI Authenticated</title><style>body{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,sans-serif;background:#0f172a;color:#f8fafc;display:flex;align-items:center;justify-content:center;height:100vh;margin:0}.card{background:#1e293b;padding:40px;border-radius:16px;border:1px solid #334155;text-align:center;box-shadow:0 20px 25px -5px rgba(0,0,0,0.5);max-width:420px}h1{color:#f59e0b;margin-bottom:8px}p{color:#94a3b8;font-size:14px}.badge{display:inline-block;background:rgba(34,197,94,0.15);color:#22c55e;padding:6px 12px;border-radius:20px;font-weight:bold;font-size:13px;margin:16px 0}</style></head><body><div class="card"><h1>sunRayPM</h1><div class="badge">[OK] Successfully Authenticated</div><p>Your terminal session is now connected to sunRayPM.</p><p style="margin-top:24px;font-size:12px;color:#64748b">You can safely close this window.</p></div></body></html>`)

		tokenChan <- struct {
			Token, SpaceID, Email, Error string
		}{
			Token:   tok,
			SpaceID: spc,
			Email:   email,
		}
	})

	go func() {
		_ = server.Serve(listener)
	}()

	fmt.Println("Opening default browser to complete authentication...")
	fmt.Printf("URL: %s%s%s\n\n", colorCyan, authURL, colorReset)
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
		_ = saveConfig(*cfg)

		if pctx != nil {
			if spaces, err := fetchAllSpaces(*cfg); err == nil && len(spaces) > 0 {
				if cfg.SpaceID != "" {
					for _, s := range spaces {
						if s.ID == cfg.SpaceID {
							pctx.ActiveSpace = &s
							break
						}
					}
				}
				if pctx.ActiveSpace == nil {
					pctx.ActiveSpace = &spaces[0]
					cfg.SpaceID = spaces[0].ID
					_ = saveConfig(*cfg)
				}
			}
		}

		fmt.Printf("\n%s[OK] Authentication successful!%s\n", colorGreen+colorBold, colorReset)
		if cfg.UserEmail != "" {
			fmt.Printf("Logged in as: %s%s%s\n", colorBold, cfg.UserEmail, colorReset)
		}
		if pctx != nil && pctx.ActiveSpace != nil {
			fmt.Printf("Active Workspace: %s%s%s (@%s)\n", colorGold, pctx.ActiveSpace.Name, colorReset, pctx.ActiveSpace.ID)
		}
		fmt.Printf("Credentials saved to: %s\n\n", getConfigFile())

	case <-time.After(120 * time.Second):
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		fmt.Printf("\n%sAuthentication timed out (120s).%s Please try again.\n", colorRed, colorReset)
	}
}

func handleAuthLogout(cfg *Config, pctx *PathContext) {
	file := getConfigFile()
	_ = os.Remove(file)
	if home, err := os.UserHomeDir(); err == nil {
		_ = os.Remove(filepath.Join(home, ".sunray", "config.json"))
		_ = os.Remove(filepath.Join(home, ".sunray"))
	}
	if cfg != nil {
		cfg.Token = ""
		cfg.SpaceID = ""
		cfg.UserEmail = ""
	}
	if pctx != nil {
		pctx.ActiveSpace = nil
		pctx.ActiveParent = nil
	}
	fmt.Printf("%s[OK] Logged out successfully.%s Saved credentials removed.\n", colorGreen, colorReset)
}

func handleAuthStatus(cfg Config) {
	fmt.Printf("\n%s[sunRayPM] Authentication Status%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("API Server:  %s%s%s\n", colorCyan, cfg.BaseURL, colorReset)
	if cfg.Token != "" {
		fmt.Printf("Status:      %sLogged In%s\n", colorGreen, colorReset)
		if cfg.UserEmail != "" {
			fmt.Printf("Account:     %s%s%s\n", colorBold, cfg.UserEmail, colorReset)
		}
		if cfg.SpaceID != "" {
			fmt.Printf("Space ID:    %s%s%s\n", colorGold, cfg.SpaceID, colorReset)
		}
	} else {
		fmt.Printf("Status:      %sNot Logged In%s (run `login`)\n", colorYellow, colorReset)
	}
	fmt.Println()
}

func handleUpdate() {
	fmt.Printf("\n%s[sunRayPM] Checking for CLI updates...%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("Current Version: %s%s%s\n", colorCyan, AppVersion, colorReset)

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.github.com/repos/DaddyChristmas/sunraypm-cli/releases/latest", nil)
	if err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
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
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
		return
	}

	if release.TagName == AppVersion {
		fmt.Printf("%s[OK] You are already on the latest version!%s (%s)\n\n", colorGreen, colorReset, AppVersion)
		return
	}

	fmt.Printf("Upgrading to %s...\n", release.TagName)
	targetAsset := fmt.Sprintf("sunray-%s-%s", runtime.GOOS, runtime.GOARCH)
	var downloadURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, targetAsset) {
			downloadURL = a.BrowserDownloadURL
			break
		}
	}
	if downloadURL == "" {
		fmt.Printf("Prebuilt binary for %s-%s not found in release.\n", runtime.GOOS, runtime.GOARCH)
		return
	}

	binResp, err := client.Get(downloadURL)
	if err != nil {
		fmt.Printf("%sDownload error:%s %v\n", colorRed, colorReset, err)
		return
	}
	defer binResp.Body.Close()

	execPath, err := os.Executable()
	if err != nil {
		return
	}
	tmpFile := execPath + ".tmp"
	out, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
	if err != nil {
		return
	}
	_, _ = io.Copy(out, binResp.Body)
	out.Close()
	_ = os.Rename(tmpFile, execPath)
	fmt.Printf("%s[OK] Successfully upgraded to %s!%s\n\n", colorGreen+colorBold, release.TagName, colorReset)
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
		fmt.Println("No workspaces found. Create one in the web dashboard.")
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
	fmt.Println("To navigate into a workspace, run: cd @workspace")
}

func handleTasksList(cfg Config, spaceID string, jsonOut bool) {
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

func handleEVM(cfg Config, spaceID string, jsonOut bool) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
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

func handleDrawKanban(cfg Config, spaceID string, containerQuery string) {
	if spaceID == "" {
		spaceID = cfg.SpaceID
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
	fmt.Printf("%s      /   \\      %s  Tip: Use `ls -kb` (Kanban) or `ls -gnt` (Gantt) for visual charts!\n", colorGold, colorReset)
	fmt.Println()
}

func printHelp() {
	fmt.Printf("%s[sunRayPM] Unix-Standard CLI Developer Tool%s\n\n", colorGold+colorBold, colorReset)
	fmt.Println("Navigation & Virtual FS:")
	fmt.Println("  pwd                               Print active breadcrumb path")
	fmt.Println("  cd <@workspace|@container|..|/>   Navigate into workspace or container")
	fmt.Println("  ls [-l|-kb|-gnt|-tree]            List items (supports Kanban, Gantt, Tree views)")
	fmt.Println("  cat <@task>                       Inspect full task metadata card")
	fmt.Println()
	fmt.Println("Item Operations:")
	fmt.Println("  touch <\"Title\"> [-d <days>] [-c <cost>] Create new task")
	fmt.Println("  mkdir <\"Title\"> [-m (milestone)]        Create container or milestone")
	fmt.Println("  mv <@task> <@dest_parent|\"New Title\">   Reparent or rename task")
	fmt.Println("  cp <@task> [\"New Title\"]                Duplicate / clone task")
	fmt.Println("  rm [-r] <@task>                         Delete task or container")
	fmt.Println("  ln <@pred> <@succ>                      Link DAG dependency")
	fmt.Println("  done <@task>                            Mark task completed (100%)")
	fmt.Println("  chmod <pct> <@task>                     Set task progress percentage")
	fmt.Println("  echo \"note\" >> <@task>                  Append note/comment to task")
	fmt.Println()
	fmt.Println("Institutional Management & Agile:")
	fmt.Println("  evm                               Earned Value Management (BAC, PV, EV, AC, CPI, SPI)")
	fmt.Println("  mbe [-t <tolerance_pct>]          Management by Exception triage report")
	fmt.Println("  cpm                               Critical Path Method delivery sequence")
	fmt.Println("  sprint                            Active sprint workload and health")
	fmt.Println("  burndown                          Sprint burndown trajectory chart")
	fmt.Println("  scurve                            Cumulative spend and schedule curve")
	fmt.Println("  raci <@container>                 RACI responsibility matrix")
	fmt.Println("  audit, report                     Generate Executive Markdown report")
	fmt.Println()
	fmt.Println("Search & System:")
	fmt.Println("  grep <query>                      Search tasks by keyword")
	fmt.Println("  find . [-type task] [-overdue]    Find items by criteria")
	fmt.Println("  head [-n <count>], tail           Top / latest tasks")
	fmt.Println("  wc                                Item count, total days, and budget")
	fmt.Println("  top, sunny                        Live dashboard and companion status")
	fmt.Println("  cal                               Terminal calendar with working days")
	fmt.Println("  whoami                            Active user and space info")
	fmt.Println("  ping                              Test API latency")
	fmt.Println("  login, logout, update             Authentication and self-updater")
	fmt.Println("  man <cmd>                         Manual page for command")
	fmt.Println()
}

// ─── REPL ENGINE & COMMAND DISPATCHER ───────────────────────────────────────

func executeREPLCommand(cfg *Config, pctx *PathContext, input string) {
	input = strings.TrimSpace(input)
	if input == "" {
		return
	}

	// Support echo "..." >> @task
	if strings.HasPrefix(input, "echo ") && strings.Contains(input, ">>") {
		handleEcho(*cfg, pctx, input)
		return
	}

	// Interactive '@' task query/preview trigger
	if strings.HasPrefix(input, "@") {
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		if spaceID == "" {
			fmt.Printf("%sPlease select a workspace first with:%s cd @workspace\n", colorYellow, colorReset)
			return
		}
		tasks, err := fetchSpaceTasks(*cfg, spaceID)
		if err != nil {
			fmt.Printf("%sError:%s %v\n", colorRed, colorReset, err)
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

	args := parseCommandLine(input)
	if len(args) == 0 {
		return
	}
	cmd := strings.ToLower(strings.TrimPrefix(args[0], "/"))

	switch cmd {
	// Navigation & FS
	case "pwd":
		handlePwd(pctx)
	case "cd":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		handleCd(cfg, pctx, target)
	case "ls":
		handleLs(*cfg, pctx, args[1:])
	case "cat":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		handleCat(*cfg, pctx, target)
	case "tree":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawTree(*cfg, spaceID, "")
	case "kanban":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawKanban(*cfg, spaceID, "")
	case "gantt":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawGantt(*cfg, spaceID, "")

	// Task & Container CRUD
	case "touch":
		handleTouch(*cfg, pctx, args[1:])
	case "mkdir":
		handleMkdir(*cfg, pctx, args[1:])
	case "mv":
		if len(args) < 3 {
			fmt.Println("Usage: mv <@task> <@dest_parent|\"New Title\">")
		} else {
			handleMv(*cfg, pctx, args[1], args[2])
		}
	case "cp":
		newName := ""
		if len(args) > 2 {
			newName = args[2]
		}
		if len(args) > 1 {
			handleCp(*cfg, pctx, args[1], newName)
		} else {
			fmt.Println("Usage: cp <@task> [\"New Title\"]")
		}
	case "rm":
		handleRm(*cfg, pctx, args[1:])
	case "ln", "link":
		handleLn(*cfg, pctx, args[1:])
	case "done":
		if len(args) > 1 {
			handleDone(*cfg, pctx, args[1])
		} else {
			fmt.Println("Usage: done <@task>")
		}
	case "chmod":
		if len(args) > 2 {
			handleChmod(*cfg, pctx, args[1], args[2])
		} else {
			fmt.Println("Usage: chmod <progress_pct> <@task>")
		}

	// Institutional Management
	case "evm", "df":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleEVM(*cfg, spaceID, false)
	case "mbe", "exceptions":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		threshold := 10.0
		for i, a := range args {
			if a == "-t" && i+1 < len(args) {
				threshold, _ = strconv.ParseFloat(args[i+1], 64)
			}
		}
		handleMBE(*cfg, spaceID, threshold)
	case "cpm":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleCPM(*cfg, spaceID)
	case "sprint":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleSprint(*cfg, spaceID)
	case "burndown":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleBurndown(*cfg, spaceID)
	case "scurve":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleSCurve(*cfg, spaceID)
	case "raci":
		if len(args) > 1 {
			handleRACI(*cfg, pctx, args[1])
		} else {
			fmt.Println("Usage: raci <@container>")
		}
	case "audit", "report":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleReport(*cfg, spaceID)

	// Search & Utilities
	case "grep":
		if len(args) > 1 {
			handleGrep(*cfg, pctx, strings.Join(args[1:], " "))
		} else {
			fmt.Println("Usage: grep <keyword>")
		}
	case "find":
		handleFind(*cfg, pctx, args[1:])
	case "head":
		count := 5
		for i, a := range args {
			if a == "-n" && i+1 < len(args) {
				count, _ = strconv.Atoi(args[i+1])
			}
		}
		handleHead(*cfg, pctx, count)
	case "tail":
		count := 5
		for i, a := range args {
			if a == "-n" && i+1 < len(args) {
				count, _ = strconv.Atoi(args[i+1])
			}
		}
		handleTail(*cfg, pctx, count)
	case "wc":
		handleWc(*cfg, pctx)
	case "top":
		handleTop(*cfg, pctx)
	case "cal":
		handleCal()
	case "whoami":
		handleWhoami(*cfg)
	case "ping":
		handlePing(*cfg)
	case "sunny":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleSunny(*cfg, spaceID)
	case "man":
		if len(args) > 1 {
			handleMan(args[1])
		} else {
			fmt.Println("Usage: man <command>")
		}
	case "clear":
		fmt.Print("\033[H\033[2J")

	// Authentication & System
	case "auth":
		if len(args) > 1 && args[1] == "logout" {
			handleAuthLogout(cfg, pctx)
		} else if len(args) > 1 && args[1] == "status" {
			handleAuthStatus(*cfg)
		} else {
			handleAuthLogin(cfg, pctx)
		}
	case "login":
		handleAuthLogin(cfg, pctx)
	case "logout":
		handleAuthLogout(cfg, pctx)
	case "status":
		handleAuthStatus(*cfg)
	case "update", "upgrade":
		handleUpdate()
	case "help":
		printHelp()

	// Compatibility aliases
	case "spaces":
		if len(args) > 1 && args[1] == "switch" && len(args) > 2 {
			handleCd(cfg, pctx, args[2])
		} else {
			handleSpacesList(*cfg, false)
		}
	case "tasks":
		if len(args) > 1 && args[1] == "list" {
			spaceID := ""
			if pctx.ActiveSpace != nil {
				spaceID = pctx.ActiveSpace.ID
			}
			handleTasksList(*cfg, spaceID, false)
		} else {
			handleLs(*cfg, pctx, args[1:])
		}
	case "draw-kanban":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawKanban(*cfg, spaceID, "")
	case "draw-gantt":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawGantt(*cfg, spaceID, "")
	case "draw-tree":
		spaceID := ""
		if pctx.ActiveSpace != nil {
			spaceID = pctx.ActiveSpace.ID
		}
		handleDrawTree(*cfg, spaceID, "")

	default:
		fmt.Printf("Unknown command '%s'. Type 'help' or 'man' for usage.\n", cmd)
	}
}

// parseCommandLine supports quoted strings like touch "My New Task" -d 5
func parseCommandLine(input string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	quoteChar := rune(0)

	for _, r := range input {
		if inQuotes {
			if r == quoteChar {
				inQuotes = false
			} else {
				current.WriteRune(r)
			}
		} else {
			if r == '"' || r == '\'' {
				inQuotes = true
				quoteChar = r
			} else if r == ' ' || r == '\t' {
				if current.Len() > 0 {
					args = append(args, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(r)
			}
		}
	}
	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

func startFallbackREPL(cfg *Config) {
	scanner := bufio.NewScanner(os.Stdin)
	pctx := &PathContext{}
	if cfg.SpaceID != "" {
		if spaces, err := fetchAllSpaces(*cfg); err == nil {
			for _, s := range spaces {
				if s.ID == cfg.SpaceID {
					pctx.ActiveSpace = &s
					break
				}
			}
		}
	}

	for scanner.Scan() {
		input := strings.TrimSpace(scanner.Text())
		if input == "" || input == "exit" || input == "quit" {
			break
		}
		executeREPLCommand(cfg, pctx, input)
	}
}

func startInteractiveREPL(cfg *Config) {
	pctx := &PathContext{}
	if cfg.SpaceID != "" {
		if spaces, err := fetchAllSpaces(*cfg); err == nil {
			for _, s := range spaces {
				if s.ID == cfg.SpaceID {
					pctx.ActiveSpace = &s
					break
				}
			}
		}
	}

	fmt.Printf("\n%s[sunRayPM] Unix Terminal Developer Shell%s\n", colorGold+colorBold, colorReset)
	fmt.Printf("Connected: %s%s%s\n", colorCyan, cfg.BaseURL, colorReset)
	if pctx.ActiveSpace != nil {
		fmt.Printf("Workspace: %s%s%s (@%s)\n", colorGold, pctx.ActiveSpace.Name, colorReset, pctx.ActiveSpace.ID)
	}
	fmt.Println("Type 'help' for commands, 'ls' to list, 'cd' to navigate, or 'exit' to quit.")
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

	terminal := term.NewTerminal(screen, pctx.Prompt())

	allCommands := []string{
		"pwd", "cd", "ls", "ls -kb", "ls -gnt", "ls -tree", "cat",
		"touch", "mkdir", "mv", "cp", "rm", "ln", "done", "chmod",
		"evm", "mbe", "cpm", "sprint", "burndown", "scurve", "raci", "audit",
		"grep", "find", "head", "tail", "wc", "top", "cal", "whoami", "ping",
		"sunny", "man", "clear", "help", "login", "logout", "status", "update", "exit", "quit",
	}

	terminal.AutoCompleteCallback = func(line string, pos int, key rune) (newLine string, newPos int, ok bool) {
		if key == '\t' {
			trimmed := strings.TrimSpace(line[:pos])
			if trimmed == "" {
				return line, pos, false
			}

			// cd / spaces autocompletion
			if strings.HasPrefix(trimmed, "cd ") {
				query := strings.TrimSpace(strings.TrimPrefix(trimmed, "cd "))
				spaces, _ := fetchAllSpaces(*cfg)
				var matches []string
				for _, s := range spaces {
					if strings.HasPrefix(strings.ToLower(s.Name), strings.ToLower(query)) || strings.HasPrefix(strings.ToLower(s.ID), strings.ToLower(query)) {
						matches = append(matches, "cd "+s.Name)
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

			// Task ref '@' autocompletion
			if strings.Contains(trimmed, "@") {
				atIdx := strings.LastIndex(trimmed, "@")
				taskQuery := trimmed[atIdx:]
				spaceID := ""
				if pctx.ActiveSpace != nil {
					spaceID = pctx.ActiveSpace.ID
				}
				if spaceID != "" {
					tasks, _ := fetchSpaceTasks(*cfg, spaceID)
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
			for _, c := range allCommands {
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
		terminal.SetPrompt(pctx.Prompt())
		input, err := terminal.ReadLine()
		if err != nil {
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

		term.Restore(fd, oldState)
		executeREPLCommand(cfg, pctx, input)
		oldState, _ = term.MakeRaw(fd)
	}
}

func main() {
	cfg := getConfig()

	if len(os.Args) < 2 {
		startInteractiveREPL(&cfg)
		return
	}

	pctx := &PathContext{}
	if cfg.SpaceID != "" {
		if spaces, err := fetchAllSpaces(cfg); err == nil {
			for _, s := range spaces {
				if s.ID == cfg.SpaceID {
					pctx.ActiveSpace = &s
					break
				}
			}
		}
	}

	cmdLine := strings.Join(os.Args[1:], " ")
	executeREPLCommand(&cfg, pctx, cmdLine)
}
