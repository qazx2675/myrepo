# Graph Report - ip_change  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 157 nodes · 359 edges · 11 communities (10 shown, 1 thin omitted)
- Extraction: 94% EXTRACTED · 6% INFERRED · 0% AMBIGUOUS · INFERRED: 23 edges (avg confidence: 0.85)
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
- Community 7
- Community 8
- Community 9
- Community 10

## God Nodes (most connected - your core abstractions)
1. `Monitor` - 23 edges
2. `run()` - 17 edges
3. `runRollback()` - 14 edges
4. `ApplyScript()` - 9 edges
5. `listLines()` - 9 edges
6. `newTestMonitor()` - 9 edges
7. `writeFakeHostname()` - 8 edges
8. `BuildCommand()` - 7 edges
9. `Run()` - 7 edges
10. `Options` - 6 edges

## Surprising Connections (you probably didn't know these)
- `runRollback()` --references--> `Config`  [EXTRACTED]
  cmd/ip-change-engine/main.go → internal/config/config.go
- `run()` --calls--> `Load()`  [EXTRACTED]
  cmd/ip-change-engine/main.go → internal/config/config.go
- `run()` --calls--> `ApplyScript()`  [EXTRACTED]
  cmd/ip-change-engine/main.go → internal/render/render.go
- `runRollback()` --calls--> `RollbackScript()`  [EXTRACTED]
  cmd/ip-change-engine/main.go → internal/render/render.go
- `run()` --calls--> `New()`  [EXTRACTED]
  cmd/ip-change-engine/main.go → internal/monitor/monitor.go

## Import Cycles
- None detected.

## Communities (11 total, 1 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.14
Nodes (20): Config, Load(), TestLoadMissingFileUsesDefaults(), TestLoadOverrides(), TestLoadRejectsUnknownKey(), spaceOut(), TestBuildCommandHasNoQuotesInPayload(), TestBuildCommandRoundTrip() (+12 more)

### Community 1 - "Community 1"
Cohesion: 0.11
Nodes (14): buildFrame(), detailLines(), fmtDuration(), listLines(), openScreen(), padRight(), pageSize(), TestDetailTruncates() (+6 more)

### Community 2 - "Community 2"
Cohesion: 0.17
Nodes (20): formatRollbackLine(), indent(), main(), run(), runRollback(), BoldCyan(), BoldRed(), Cyan() (+12 more)

### Community 3 - "Community 3"
Cohesion: 0.14
Nodes (9): Monitor, New(), newTestMonitor(), TestAbortSummary(), TestLastPageShort(), TestSnapshotStates(), TestStderrThenStdoutUsesStdout(), hostState (+1 more)

### Community 4 - "Community 4"
Cohesion: 0.13
Nodes (9): isTerminal(), readKeys(), ioctl(), makeRaw(), setProcessGroup(), ttyRows(), makeRaw(), setProcessGroup() (+1 more)

### Community 5 - "Community 5"
Cohesion: 0.47
Nodes (5): formatResultLine(), ipMonitorLabel(), TestFormatResultLineFail(), TestFormatResultLineMalformed(), TestFormatResultLineOK()

### Community 6 - "Community 6"
Cohesion: 0.80
Nodes (4): detect_os_major(), fail_out(), set_ifcfg_key(), apply_body.sh script

### Community 7 - "Community 7"
Cohesion: 0.83
Nodes (3): detect_os_major(), fail_out(), rollback_body.sh script

### Community 8 - "Community 8"
Cohesion: 0.83
Nodes (3): select_user_context(), run_ip_change.sh script, usage()

### Community 9 - "Community 9"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

## Knowledge Gaps
- **3 isolated node(s):** `ip-change`, `GOFLAGS`, `GOPROXY`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 26 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Monitor` connect `Community 3` to `Community 1`, `Community 2`, `Community 4`?**
  _High betweenness centrality (0.150) - this node is a cross-community bridge._
- **What connects `ip-change`, `GOFLAGS`, `GOPROXY` to the rest of the system?**
  _3 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.13968253968253969 - nodes in this community are weakly interconnected._
- **Why does `run()` connect `Community 2` to `Community 0`, `Community 3`, `Community 5`?**
  _High betweenness centrality (0.067) - this node is a cross-community bridge._
- **Should `Community 1` be split into smaller, more focused modules?**
  _Cohesion score 0.11088709677419355 - nodes in this community are weakly interconnected._
- **Why does `Options` connect `Community 2` to `Community 1`, `Community 3`?**
  _High betweenness centrality (0.036) - this node is a cross-community bridge._
- **Should `Community 3` be split into smaller, more focused modules?**
  _Cohesion score 0.1380952380952381 - nodes in this community are weakly interconnected._