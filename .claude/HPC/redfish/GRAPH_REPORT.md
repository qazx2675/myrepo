# Graph Report - redfish  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 908 nodes · 3658 edges · 29 communities (23 shown, 6 thin omitted)
- Extraction: 79% EXTRACTED · 21% INFERRED · 0% AMBIGUOUS · INFERRED: 765 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `8518724a`
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
- Community 11
- Community 12
- Community 13
- Community 14
- Community 15
- Community 16
- Community 17
- Community 18
- Community 19
- Community 20
- Community 21
- Community 22
- Community 23
- Community 24
- Community 25
- Community 26
- Community 27
- Community 28

## God Nodes (most connected - your core abstractions)
1. `startMockTree()` - 71 edges
2. `must()` - 63 edges
3. `Host` - 43 edges
4. `checkRCFor()` - 42 edges
5. `checkRun` - 35 edges
6. `Client` - 34 edges
7. `runCheckP()` - 32 edges
8. `Server` - 30 edges
9. `setProf()` - 29 edges
10. `runDump()` - 29 edges

## Surprising Connections (you probably didn't know these)
- `TestScaleCheckThousands()` --calls--> `loadTree()`  [INFERRED]
  cmd/biostool/scale_test.go → internal/mockbmc/mockbmc.go
- `main()` --calls--> `New()`  [EXTRACTED]
  cmd/mockbmc/main.go → internal/mockbmc/mockbmc.go
- `mockSnap` --references--> `SessionStats`  [EXTRACTED]
  cmd/biostool/set_test.go → internal/mockbmc/mockbmc.go
- `allScenario` --references--> `Server`  [EXTRACTED]
  cmd/biostool/allcheck_test.go → internal/mockbmc/mockbmc.go
- `setAttrs()` --references--> `Server`  [EXTRACTED]
  cmd/biostool/allcheck_test.go → internal/mockbmc/mockbmc.go

## Import Cycles
- None detected.

## Communities (29 total, 6 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.10
Nodes (88): applyRow, failReader, itemResult, link, mockSnap, assertClean(), assertOneSession(), checkRCFor() (+80 more)

### Community 1 - "Community 1"
Cohesion: 0.08
Nodes (56): aggEntry, allGroup, allReportOpts, allRun, checkRun, hostState, noteGroup, painter (+48 more)

### Community 2 - "Community 2"
Cohesion: 0.06
Nodes (52): attrCand, attrRegistry, checkEnv, checkOpts, getFunc, hostCheck, memoGet, memoRes (+44 more)

### Community 3 - "Community 3"
Cohesion: 0.07
Nodes (38): Client, credentials, failItem, Mode, setEnv, setHost, setItem, setPlan (+30 more)

### Community 4 - "Community 4"
Cohesion: 0.08
Nodes (51): allEnv, allHost, allOpts, allRef, allScenario, attrDiffRow, biosSnap, hostNote (+43 more)

### Community 5 - "Community 5"
Cohesion: 0.10
Nodes (50): dumpMeta, dumpOpts, dumpRes, dumpResult, dumpSession, hostSummary, appendIndex(), candText() (+42 more)

### Community 6 - "Community 6"
Cohesion: 0.09
Nodes (46): ClientOpts, mockBMC, mockCounts, sleepRec, TestMockAuthFailStopsImmediately(), TestMockSlowResponseIsTimeout(), backoffFor(), NewClient() (+38 more)

### Community 7 - "Community 7"
Cohesion: 0.13
Nodes (34): CallStats, hostOutcome, resSampler, TestAllCheckAuthStop(), TestAllCheckConcurrentFarm(), fastCheck(), mib(), openFilesLimit() (+26 more)

### Community 8 - "Community 8"
Cohesion: 0.19
Nodes (32): Options, New(), collectLinks(), mustWrite(), newCli(), newSrv(), patchBody(), startLogin() (+24 more)

### Community 9 - "Community 9"
Cohesion: 0.18
Nodes (25): rtEnv, testProf(), checkRowsState(), containsLine(), mustRead(), newRTEnv(), rtAll(), rtCapture() (+17 more)

### Community 10 - "Community 10"
Cohesion: 0.08
Nodes (7): canonIP(), Farm, FarmOptions, NewFarm(), selfSignedCert(), countConn, farmListener

### Community 11 - "Community 11"
Cohesion: 0.16
Nodes (18): decrypt(), encrypt(), encryptFromReader(), loadKey(), loadOrCreateKey(), newGCM(), readPassword(), TestBadKeyLength() (+10 more)

### Community 12 - "Community 12"
Cohesion: 0.17
Nodes (18): Profile, profRow, normalizeModel(), listProfileNames(), loadProfile(), parseProfile(), mustProfile(), tabs() (+10 more)

### Community 13 - "Community 13"
Cohesion: 0.15
Nodes (20): hasSuffixFold(), loadList(), parseHosts(), parseList(), resolveTargets(), splitPort(), splitTargets(), TestParseHosts() (+12 more)

### Community 14 - "Community 14"
Cohesion: 0.12
Nodes (11): Fault, Tree, LoadTree(), loadTree(), newHost(), validateAttr(), attrErr, faultRule (+3 more)

### Community 15 - "Community 15"
Cohesion: 0.21
Nodes (5): isLogPath(), mustJSON(), writeErr(), writeJSON(), statusWriter

### Community 16 - "Community 16"
Cohesion: 0.16
Nodes (5): normPath(), orStr(), Call, Host, Job

### Community 17 - "Community 17"
Cohesion: 0.21
Nodes (14): Config, defaultConfig(), loadConf(), parseConf(), parseIntAtLeast(), setIfNotEmpty(), TestLoadConf(), TestParseConfAllKeys() (+6 more)

### Community 18 - "Community 18"
Cohesion: 0.21
Nodes (12): opt_val(), all_bios_check.sh script, usage(), opt_val(), bios_check.sh script, usage(), encrypt.sh script, conf_val() (+4 more)

### Community 19 - "Community 19"
Cohesion: 0.28
Nodes (13): commonOpts, notImplementedError, runContext, cmdAllCheck(), cmdCheck(), cmdEncrypt(), main(), newFlagSet() (+5 more)

### Community 20 - "Community 20"
Cohesion: 0.33
Nodes (7): find_src(), is_keep(), main(), newest_redfish_zip(), update.sh script, sum_of(), update_profiles()

### Community 21 - "Community 21"
Cohesion: 0.71
Nodes (6): farmCli(), mustTree(), newFarmT(), TestFarmPerHostCredsFaultsAndConnCounts(), TestFarmSeparatesHostsByLocalIP(), TestFarmUnknownIPDroppedOrStatus()

### Community 24 - "Community 24"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

## Knowledge Gaps
- **4 isolated node(s):** `GOFLAGS`, `GOPROXY`, `PATH`, `biostool`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 64 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **6 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Host` connect `Community 16` to `Community 0`, `Community 6`, `Community 8`, `Community 10`, `Community 14`, `Community 15`, `Community 23`?**
  _High betweenness centrality (0.047) - this node is a cross-community bridge._
- **Are the 58 inferred relationships involving `startMockTree()` (e.g. with `startAllSrv()` and `TestAllowedValuesAndModelFallback()`) actually correct?**
  _`startMockTree()` has 58 INFERRED edges - model-reasoned connections that need verification._
- **What connects `GOFLAGS`, `GOPROXY`, `PATH` to the rest of the system?**
  _4 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.09881576392933411 - nodes in this community are weakly interconnected._
- **Why does `Client` connect `Community 3` to `Community 0`, `Community 2`, `Community 5`, `Community 6`, `Community 7`?**
  _High betweenness centrality (0.036) - this node is a cross-community bridge._
- **Are the 50 inferred relationships involving `must()` (e.g. with `.writeFile()` and `checkAllFiles()`) actually correct?**
  _`must()` has 50 INFERRED edges - model-reasoned connections that need verification._
- **Should `Community 1` be split into smaller, more focused modules?**
  _Cohesion score 0.08148148148148149 - nodes in this community are weakly interconnected._