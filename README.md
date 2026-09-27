# sunRayPM CLI (`sunraypm`)

> Fast, lightweight, standalone Unix-standard terminal client & institutional management tool for **[sunRayPM](https://sunraypm.com)**.

[![Go Version](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

---

## 🚀 Quick Install

### Via Go
```bash
go install github.com/DaddyChristmas/sunraypm-cli@latest
```

### Via Binary Installer
```bash
curl -fsSL https://raw.githubusercontent.com/DaddyChristmas/sunraypm-cli/main/install.sh | bash
```

---

## 💻 Unix Filesystem Workflow

sunRayPM CLI treats your nested workspace hierarchy like a virtual POSIX filesystem.

```text
sunray [SunRay Aerospace]❯ pwd
/SunRay Technologies/Orbital Spacecraft/@Propulsion Stage

sunray [SunRay Aerospace]❯ ls -kb
[sunRayPM] Terminal Kanban Board • Total: 7 tasks

=== [ ] TO DO (2) =========================================
  ┌─ @10c2e3 Orbital Launch Milestone
  │  Dur:  0d  •  Cost: $ 0    •  [░░░░░░░░░░]   0%
  └─────────────────────────────────────────────────────

=== [~] IN PROGRESS (3) ===================================
  ┌─ @0dca36 Avionics Firmware Telemetry
  │  Dur:  3d  •  Cost: $ 600  •  [██████░░░░]  60%
  └─────────────────────────────────────────────────────

=== [x] DONE (2) =========================================
  ┌─ @8f2b10 Rocket Engine Stage 1 Static Fire
  │  Dur:  5d  •  Cost: $1200  •  [██████████] 100%
  └─────────────────────────────────────────────────────
```

---

## 🎯 Fuzzy Task Reference Selector (`@`)

Like modern developer command interfaces, typing `@` or pressing `Tab` inside the interactive shell pops up an interactive task selector box displaying short IDs, types, live progress bars, status badges, and costs:

```text
sunray [SunRay Aerospace]❯ @
  ┌── Select Task Reference (@...) ─────────────────────────────────────────────
  │  REF        TYPE TITLE                      PROGRESS       STATUS         
  ├───────────────────────────────────────────────────────────────────────────
  │  @0dca36    [T]  Avionics Firmware Telemetry[ 60%]         [~] IN PROGRESS
  │  @8f2b10    [C]  Propulsion Stage Engine    [100%]         [x] DONE       
  │  @10c2e3    [M]  Orbital Launch Milestone   [  0%]         [ ] TO DO      
  └───────────────────────────────────────────────────────────────────────────
```

Typing a specific task reference (e.g. `@0dca36` or `cat @0dca36`) renders the full task inspection card:

```text
   ┌── @0dca36 ──────────────────────────────────────────────
   │ Implement OAuth2 Refresh
   │ Status:   [IN PROGRESS]  Progress: [██████░░░░] (60%)
   │ Duration: 3d  •  Cost: $600
   │ Dates:    2026-09-24 → 2026-09-27
   └─────────────────────────────────────────────────────────
```

---

## 🛠️ Complete Command Reference

### 1. Navigation & Virtual Filesystem
| Command | Syntax | Description |
| :--- | :--- | :--- |
| **`pwd`** | `pwd` | Print active hierarchical DAG breadcrumb path (`/Org/Space/@Parent`) |
| **`cd`** | `cd <@space|@container|..|/>` | Navigate into workspace, container, parent level (`..`), or root (`/`) |
| **`ls`** | `ls [-kb|-gnt|-tree]` | List items in tabular, Kanban (`-kb`), Gantt (`-gnt`), or Tree (`-tree`) view |
| **`cat`** | `cat <@task>` | Inspect full task metadata card with progress, assignees, dates, and cost |

### 2. Item & Container CRUD Operations
| Command | Syntax | Description |
| :--- | :--- | :--- |
| **`touch`** | `touch "Title" [-d <days>] [-c <cost>]` | Create new task in active container/workspace |
| **`mkdir`** | `mkdir "Title" [-m]` | Create new container or milestone (`-m`) |
| **`mv`** | `mv <@task> <@dest_parent|"New Title">` | Reparent task to another container or rename |
| **`cp`** | `cp <@task> ["New Title"]` | Duplicate / clone task |
| **`rm`** | `rm [-r] <@task>` | Delete task or recursively delete container branch (`-r`) |
| **`ln`** | `ln <@pred> <@succ>` | Link DAG dependency edge |
| **`done`** | `done <@task>` | Quickly mark task 100% completed |
| **`chmod`** | `chmod <0-100> <@task>` | Set task progress percentage |
| **`echo`** | `echo "note" >> <@task>` | Append note or description to task |

### 3. Institutional Management & Analytics
| Command | Syntax | Description |
| :--- | :--- | :--- |
| **`evm`** / **`df`** | `evm` | Real-time Earned Value Management (BAC, PV, EV, AC, CPI, SPI, EAC, VAC) |
| **`scurve`** | `scurve` | Render cumulative spend and schedule delivery trajectory curve |
| **`mbe`** | `mbe [-t <tolerance_pct>]` | Management by Exception triage (flags tasks breaching variance threshold) |
| **`cpm`** | `cpm` | Determine and highlight the Critical Path delivery sequence |
| **`sprint`** | `sprint` | Active sprint workload, velocity, and health |
| **`burndown`** | `burndown` | ASCII sprint burndown trajectory chart |
| **`raci`** | `raci <@container>` | Render RACI Responsibility Assignment Matrix |
| **`audit`** / **`report`** | `report` | Generate 1-Click Executive Markdown project report |

### 4. Search, Utilities & Diagnostics
| Command | Syntax | Description |
| :--- | :--- | :--- |
| **`grep`** | `grep <query>` | Search tasks and descriptions by keyword |
| **`find`** | `find . [-type task] [-overdue]` | Filter tasks by attributes |
| **`head`** / **`tail`** | `head [-n 5]` / `tail` | View top or latest tasks |
| **`wc`** | `wc` | Print count of items, total duration days, and total budget |
| **`top`** / **`sunny`** | `top` / `sunny` | Live workspace dashboard and companion mascot status |
| **`cal`** | `cal` | Terminal calendar highlighting configured working days |
| **`whoami`** / **`ping`** | `whoami` / `ping` | View logged-in account, active workspace, and API latency |
| **`login`** / **`logout`** | `login` / `logout` | Browser authentication and credential cleanup |
| **`update`** | `update` | Self-updater for latest release |

---

## ⚙️ Configuration & Environment

Credentials are automatically stored in `~/.sunraypm/config.json`.

You can also override settings via environment variables:

```bash
export SUNRAYPM_API_URL="https://api.sunraypm.com" # Default: https://api.sunraypm.com
export SUNRAYPM_TOKEN="<your_jwt_token>"
export SUNRAYPM_SPACE_ID="<default_space_id>"
```

---

## 📄 License & Intellectual Property

- This client utility (`sunraypm-cli`) is open source under the **MIT License**.
- **sunRayPM** and the core sunRayPM cloud platform, scheduling engines, and server architecture are Copyright © 2026 sunRayPM (DaddyChristmas). All rights reserved.
