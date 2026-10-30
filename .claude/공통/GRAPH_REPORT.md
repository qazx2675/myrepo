# Graph Report - 공통  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 136 nodes · 297 edges · 18 communities (12 shown, 6 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 9 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `f1a33642`
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
- Community 12
- Community 13
- Community 14
- Community 15
- Community 16
- Community 17

## God Nodes (most connected - your core abstractions)
1. `main()` - 23 edges
2. `runSSHCommand()` - 17 edges
3. `hostCtl` - 10 edges
4. `writeRPMReport()` - 9 edges
5. `emit()` - 8 edges
6. `planSpecial()` - 8 edges
7. `compressHosts()` - 8 edges
8. `startInterruptHandler()` - 8 edges
9. `runSSHCommand()` - 8 edges
10. `renderHangListLocked()` - 8 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `planRPM()`  [INFERRED]
  gossh/main.go → gossh/rpmcmp.go
- `main()` --calls--> `planSpecial()`  [INFERRED]
  gossh/main.go → gossh/tmplimit.go
- `writeRPMReport()` --calls--> `compressHosts()`  [INFERRED]
  gossh/rpmcmp.go → gossh/main.go
- `main()` --calls--> `writeRPMReport()`  [INFERRED]
  gossh/main.go → gossh/rpmcmp.go
- `writeRPMReport()` --calls--> `displayWidth()`  [INFERRED]
  gossh/rpmcmp.go → gossh/main.go

## Import Cycles
- None detected.

## Communities (18 total, 6 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.20
Nodes (12): hostCtl, colorize(), colorizeOut(), emit(), handleCanceled(), lookupHostWithRetry(), printCommandResult(), printPdshStyle() (+4 more)

### Community 1 - "Community 1"
Cohesion: 0.21
Nodes (7): planRPM(), buildLimitScript(), buildTmpScript(), fetchRemoteCmd(), fetchRemoteFile(), planSpecial(), promptLine()

### Community 2 - "Community 2"
Cohesion: 0.16
Nodes (8): runPmCheck(), expandHostLine(), getAuthMethods(), isAnacondaRunning(), main(), printPdshStyle(), runSSHCommand(), writeHostsToFile()

### Community 3 - "Community 3"
Cohesion: 0.20
Nodes (9): dedupeSortedInts(), expandHostLine(), foldRangeSet(), foldRangeSetBracketed(), formatNum(), hasLetter(), mergeRangeTokens(), stripANSI() (+1 more)

### Community 4 - "Community 4"
Cohesion: 0.18
Nodes (6): hostTiming, writeHostsToFile(), diffPkgs(), parsePkgSet(), writeRPMReport(), rpmGroup

### Community 5 - "Community 5"
Cohesion: 0.31
Nodes (9): hangRow, collectHangRows(), displayWidth(), fitWidth(), fmtDuration(), isWideRune(), padRight(), printUsage() (+1 more)

### Community 6 - "Community 6"
Cohesion: 0.33
Nodes (7): countHangRunning(), getAuthMethods(), isAutofsUserPath(), main(), preprocessArgs(), restoreTerminal(), startTerminalGuard()

### Community 7 - "Community 7"
Cohesion: 0.53
Nodes (5): FOLDER_DESC, get_description(), is_excluded(), print_tree(), dirmap.sh script

### Community 8 - "Community 8"
Cohesion: 0.33
Nodes (6): drawProgressLocked(), enterListLocked(), leaveListIfActive(), leaveListLocked(), progressLineLocked(), startMonitorKeys()

### Community 9 - "Community 9"
Cohesion: 0.50
Nodes (4): emitLocked(), isCharDevice(), isTTYFile(), pendingWrite

### Community 12 - "Community 12"
Cohesion: 0.50
Nodes (4): compressHosts(), printBunched(), printUnreachableGroup(), splitTrailingDigits()

### Community 13 - "Community 13"
Cohesion: 1.00
Nodes (3): abortAll(), cancelActiveHosts(), startInterruptHandler()

## Knowledge Gaps
- **3 isolated node(s):** `gossh`, `gossh`, `FOLDER_DESC`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 21 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **6 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `main()` connect `Community 6` to `Community 0`, `Community 1`, `Community 3`, `Community 4`, `Community 5`, `Community 8`, `Community 9`, `Community 12`, `Community 13`?**
  _High betweenness centrality (0.058) - this node is a cross-community bridge._
- **Are the 3 inferred relationships involving `main()` (e.g. with `planRPM()` and `writeRPMReport()`) actually correct?**
  _`main()` has 3 INFERRED edges - model-reasoned connections that need verification._
- **What connects `gossh`, `gossh`, `FOLDER_DESC` to the rest of the system?**
  _3 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Why does `runSSHCommand()` connect `Community 0` to `Community 1`, `Community 2`, `Community 3`, `Community 6`?**
  _High betweenness centrality (0.056) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `writeRPMReport()` (e.g. with `main()` and `compressHosts()`) actually correct?**
  _`writeRPMReport()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **Why does `hostCtl` connect `Community 0` to `Community 3`?**
  _High betweenness centrality (0.044) - this node is a cross-community bridge._