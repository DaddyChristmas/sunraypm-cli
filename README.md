# sunRayPM CLI (`sunraypm`)

> Fast, lightweight, standalone terminal client & ASCII visualizer for **[sunRayPM](https://sunraypm.com)**.

[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## Quick Install

Install instantly via the terminal installer:

```bash
curl -fsSL https://raw.githubusercontent.com/DaddyChristmas/sunraypm-cli/main/install.sh | bash
```

Or install with Go:

```bash
go install github.com/DaddyChristmas/sunraypm-cli@latest
```

---

## Visualizer Commands (Programmer Tools)

### 1. ASCII Kanban Board
Render a live 3-column Kanban board right inside your terminal:
```bash
sunray /draw-kanban @task
# or scoped to a container:
sunray /draw-kanban @containerxyz
```

```text
[sunRayPM] Terminal Kanban Board • Total: 9 tasks

=== [ ] TO DO (3) =========================================
  ┌─ @e41a82f9 Implement OAuth2 Refresh
  │  Dur:  3d  •  Cost: $ 600  •  [░░░░░░░░░░]   0%
  └─────────────────────────────────────────────────────

=== [~] IN PROGRESS (4) ===================================
  ┌─ @8f2b10a1 Database DAG CTE Engine
  │  Dur:  5d  •  Cost: $1200  •  [██████░░░░]  60%
  └─────────────────────────────────────────────────────

=== [x] DONE (2) =========================================
  ┌─ @10c2e399 Landing Page Revamp
  │  Dur:  2d  •  Cost: $ 400  •  [██████████] 100%
  └─────────────────────────────────────────────────────
```

### 2. ASCII Gantt Schedule Timeline
Render a horizontal Gantt chart in the console:
```bash
sunray /draw-gantt @space
```

```text
[sunRayPM] Terminal Gantt Timeline (Tasks: 4)

 TASK / CONTAINER         | Days (1 to 20)
 ------------------------ | 01  03  05  07  09  11  13  15  17  19
--------------------------+-----------------------------------------
@e41a82 Implement Auth    | [========>]
@8f2b10 Backend DAG CTE   |       [===================>]
@10c2e3 Landing Page      |             [========>]
@99ef01 Launch Milestone  |                   [M] (Milestone)
--------------------------+-----------------------------------------
```

### 3. Container & Subtask Tree Hierarchy
```bash
sunray /draw-tree @parent
```

```text
[sunRayPM] Container & Task Tree Structure

└── [C] Backend Redesign (@8f2b10 - 45%)
    ├── [T] Database Schema (@e41a82 - 100%)
    ├── [T] API Endpoints (@99ef01 - 30%)
    └── [M] v1.0 Launch (@10c2e3 - 0%)
```

### 4. Sunny Mascot & Daily Companion
```bash
sunray /sunny
```

```text
      \   /
       .-.         Sunny (Project Companion):
    ― ( O O ) ―    "Keep crushing your sprint milestones!"
       `-´         Workspace Status: 12 Completed • 4 In Flight
      /   \        Tip: Use 'sunray draw-kanban' or '/draw-gantt' for terminal charts!
```

---

## Interactive REPL Shell

Launch an interactive shell with native raw terminal mode (Up/Down history, Tab autocompletion, prompt protection, and live terminal card previews):

```bash
sunray repl
```

Inside the REPL:
```text
[sunRayPM] Interactive REPL Shell
Connected to: https://sunraypm.com
Active Space: spc_99812401

sunray❯ @auth
Matching Tasks (1 found):
   ┌── @e41a82f9 ──────────────────────────────────────────────
   │ Implement Auth
   │ Status:   [IN PROGRESS]  Progress: [██████░░░░] (60%)
   │ Duration: 3d  •  Cost: $600
   │ Dates:    2026-09-24 → 2026-09-27
   └─────────────────────────────────────────────────────────
```

---

## 🛠️ CLI Command Reference

| Command | Syntax | Description |
| :--- | :--- | :--- |
| **Auth Login**  | `sunray auth login` (or `sunray login`) | Authenticate via default browser |
| **Auth Logout** | `sunray auth logout` | Remove stored credentials |
| **Auth Status** | `sunray auth status` | View active user and space status |
| **Spaces** | `sunray spaces list` | List all accessible workspaces |
| **Tasks List** | `sunray tasks list --space-id <ID>` | List tasks with progress & cost metrics |
| **Tasks Create**| `sunray tasks create --space-id <ID> --name "Task" --duration 3` | Create new container or task |
| **Tasks Update**| `sunray tasks update @task --progress 80` | Update task progress / duration |
| **Tasks Delete**| `sunray tasks delete @task` | Delete task |
| **Dependencies**| `sunray deps add --pred @t1 --succ @t2` | Create DAG dependency edge |
| **EVM Metrics** | `sunray evm --space-id <ID>` | Calculate BAC, PV, EV, AC, CPI, SPI |
| **Executive Report**| `sunray report --space-id <ID>` | Generate formatted Markdown audit report |
| **Self-Update** | `sunray update` (or `sunray upgrade`) | Upgrade CLI to latest release |
| **Kanban Draw** | `sunray /draw-kanban [@container]` | Render 3-column ASCII Kanban |
| **Gantt Draw**  | `sunray /draw-gantt [@container]` | Render ASCII Gantt timeline |
| **Tree Draw**   | `sunray /draw-tree [@container]` | Render ASCII task hierarchy |
| **Sunny Mascot**| `sunray /sunny` | Project pet companion status |

---

## ⚙️ Configuration

Credentials are automatically saved to `~/.sunray/config.json` when running `sunray auth login`.

You can also configure via environment variables:

```bash
export SUNRAY_API_URL="https://api.sunraypm.com" # Default: https://api.sunraypm.com
export SUNRAY_TOKEN="<your_zitadel_jwt_token>"
export SUNRAY_SPACE_ID="<default_space_id>"
```

---

## 📄 License & Intellectual Property

- This client utility (`sunraypm-cli`) is open source under the **MIT License**.
- **sunRayPM** and the core sunRayPM cloud platform, scheduling engines, server architecture, and proprietary algorithms are Copyright © 2026 sunRayPM (DaddyChristmas). All rights reserved.
