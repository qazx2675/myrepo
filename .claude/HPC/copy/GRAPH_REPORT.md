# Graph Report - copy  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 80 nodes · 150 edges · 7 communities (5 shown, 2 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 6 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a99e0065`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- Community 0
- Community 1
- Community 2
- Community 3
- Community 4
- Community 5
- Community 6

## God Nodes (most connected - your core abstractions)
1. `Client` - 11 edges
2. `Load()` - 9 edges
3. `handleFetch()` - 8 edges
4. `wndProc()` - 7 edges
5. `writeTempConf()` - 7 edges
6. `run()` - 5 edges
7. `NewClient()` - 5 edges
8. `ResolvePath()` - 5 edges
9. `point` - 4 edges
10. `JobTemplate` - 4 edges

## Surprising Connections (you probably didn't know these)
- `run()` --calls--> `Load()`  [EXTRACTED]
  cmd/copy-send/main.go → internal/config/config.go
- `run()` --calls--> `ResolvePath()`  [EXTRACTED]
  cmd/copy-send/main.go → internal/config/config.go
- `handleFetch()` --calls--> `NewClient()`  [EXTRACTED]
  cmd/copy-widget/main_windows.go → internal/awx/client.go
- `handleFetch()` --calls--> `Load()`  [EXTRACTED]
  cmd/copy-widget/main_windows.go → internal/config/config.go
- `handleFetch()` --calls--> `ResolvePath()`  [EXTRACTED]
  cmd/copy-widget/main_windows.go → internal/config/config.go

## Import Cycles
- None detected.

## Communities (7 total, 2 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.13
Nodes (4): APIError, main(), run(), NewClient()

### Community 1 - "Community 1"
Cohesion: 0.23
Nodes (13): abs32(), getCursorPos(), getWindowOrigin(), handleFetch(), onPaint(), setClipboardText(), showMessage(), wndProc() (+5 more)

### Community 2 - "Community 2"
Cohesion: 0.24
Nodes (11): Config, Load(), parseBool(), ResolvePath(), TestLoadBadSyntax(), TestLoadDefaultMaxLines(), TestLoadMissingRequired(), TestLoadValid() (+3 more)

### Community 3 - "Community 3"
Cohesion: 0.30
Nodes (3): Client, JobTemplate, jobTemplateList

### Community 4 - "Community 4"
Cohesion: 0.33
Nodes (4): send.sh script, GOPROXY, GOTOOLCHAIN, setup.sh script

## Knowledge Gaps
- **3 isolated node(s):** `GOPROXY`, `GOTOOLCHAIN`, `copy-bridge`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 20 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **2 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Client` connect `Community 3` to `Community 0`?**
  _High betweenness centrality (0.174) - this node is a cross-community bridge._
- **Are the 4 inferred relationships involving `Load()` (e.g. with `TestLoadBadSyntax()` and `TestLoadDefaultMaxLines()`) actually correct?**
  _`Load()` has 4 INFERRED edges - model-reasoned connections that need verification._
- **What connects `GOPROXY`, `GOTOOLCHAIN`, `copy-bridge` to the rest of the system?**
  _3 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.13157894736842105 - nodes in this community are weakly interconnected._
- **Why does `authTransport` connect `Community 5` to `Community 0`?**
  _High betweenness centrality (0.089) - this node is a cross-community bridge._
- **Why does `NewClient()` connect `Community 0` to `Community 1`, `Community 3`?**
  _High betweenness centrality (0.069) - this node is a cross-community bridge._