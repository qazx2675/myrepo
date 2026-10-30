# Graph Report - ldap_setting  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 216 nodes · 497 edges · 17 communities (14 shown, 3 thin omitted)
- Extraction: 92% EXTRACTED · 8% INFERRED · 0% AMBIGUOUS · INFERRED: 40 edges (avg confidence: 0.85)
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
- Community 11
- Community 12
- Community 13
- Community 14
- Community 15
- Community 16

## God Nodes (most connected - your core abstractions)
1. `Monitor` - 23 edges
2. `run()` - 13 edges
3. `ApplyScript()` - 12 edges
4. `RollbackScript()` - 11 edges
5. `Load()` - 11 edges
6. `runRollback()` - 10 edges
7. `write()` - 10 edges
8. `Infra` - 9 edges
9. `listLines()` - 9 edges
10. `newTestMonitor()` - 9 edges

## Surprising Connections (you probably didn't know these)
- `opts` --references--> `RollbackMode`  [EXTRACTED]
  cmd/ldap-config-engine/main.go → internal/render/render.go
- `loadTargets()` --references--> `Entry`  [EXTRACTED]
  cmd/ldap-config-engine/main.go → internal/asset/asset.go
- `run()` --calls--> `New()`  [EXTRACTED]
  cmd/ldap-config-engine/main.go → internal/monitor/monitor.go
- `loadTargets()` --calls--> `Load()`  [EXTRACTED]
  cmd/ldap-config-engine/main.go → internal/asset/asset.go
- `loadTargets()` --calls--> `LoadHostList()`  [EXTRACTED]
  cmd/ldap-config-engine/main.go → internal/asset/asset.go

## Import Cycles
- None detected.

## Communities (17 total, 3 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.09
Nodes (23): buildFrame(), detailLines(), fmtDuration(), Monitor, listLines(), New(), openScreen(), padRight() (+15 more)

### Community 1 - "Community 1"
Cohesion: 0.12
Nodes (19): indent(), loadTargets(), main(), modeLabel(), run(), runRollback(), BuildCommand(), Options (+11 more)

### Community 2 - "Community 2"
Cohesion: 0.16
Nodes (18): ApplyScript(), boolVar(), RollbackMode, hasService(), RollbackScript(), infra(), TestApplyScriptEmbedsValues(), TestApplyScriptIncludesLib() (+10 more)

### Community 3 - "Community 3"
Cohesion: 0.13
Nodes (8): readKeys(), ioctl(), makeRaw(), setProcessGroup(), ttyRows(), makeRaw(), setProcessGroup(), ttyRows()

### Community 4 - "Community 4"
Cohesion: 0.25
Nodes (16): Entry, GroupBySite(), Load(), LoadHostList(), StripEV(), TestDuplicateHostLastWins(), TestGroupBySite(), TestLoadHostList() (+8 more)

### Community 5 - "Community 5"
Cohesion: 0.32
Nodes (7): Config, Site, build(), buildInfra(), Infra, S4Rule, splitList()

### Community 6 - "Community 6"
Cohesion: 0.38
Nodes (10): apply_auto_appl(), apply_autofs_auth(), apply_sudo_ldap_uri(), commit_file(), set_directive(), set_ini_in_section(), set_kv(), set_line_list() (+2 more)

### Community 7 - "Community 7"
Cohesion: 0.49
Nodes (9): Load(), TestBadLineRejected(), TestDefaultSiteParsed(), TestDuplicateStorageRejected(), TestLoadGood(), TestMissingKeyRejected(), TestUnknownURIKeyRejected(), TestURIListSkipsNONE() (+1 more)

### Community 8 - "Community 8"
Cohesion: 0.36
Nodes (7): changed_has(), fail(), log(), mark_changed(), restart_for_changed(), restart_svc(), lib_common.sh script

### Community 9 - "Community 9"
Cohesion: 0.46
Nodes (7): activate(), apply(), check(), make_fixture(), ng(), ok(), test_all.sh script

### Community 10 - "Community 10"
Cohesion: 0.83
Nodes (3): all_stamps(), restore_one(), rollback_body.sh script

### Community 11 - "Community 11"
Cohesion: 0.83
Nodes (3): select_user_context(), deploy_ldap.sh script, usage()

### Community 12 - "Community 12"
Cohesion: 0.83
Nodes (3): select_infra(), select_user_context(), run.sh script

### Community 13 - "Community 13"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

## Knowledge Gaps
- **3 isolated node(s):** `GOFLAGS`, `GOPROXY`, `ldap-automation`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 35 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **3 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Monitor` connect `Community 0` to `Community 1`, `Community 3`?**
  _High betweenness centrality (0.088) - this node is a cross-community bridge._
- **Are the 5 inferred relationships involving `ApplyScript()` (e.g. with `TestApplyScriptEmbedsValues()` and `TestApplyScriptIncludesLib()`) actually correct?**
  _`ApplyScript()` has 5 INFERRED edges - model-reasoned connections that need verification._
- **What connects `GOFLAGS`, `GOPROXY`, `ldap-automation` to the rest of the system?**
  _3 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.09090909090909091 - nodes in this community are weakly interconnected._
- **Why does `run()` connect `Community 1` to `Community 0`, `Community 2`, `Community 4`, `Community 7`?**
  _High betweenness centrality (0.028) - this node is a cross-community bridge._
- **Are the 6 inferred relationships involving `RollbackScript()` (e.g. with `TestNoRestartOnlyAddsHeaderAndOverride()` and `TestRollbackNoRestartSkipsServices()`) actually correct?**
  _`RollbackScript()` has 6 INFERRED edges - model-reasoned connections that need verification._
- **Should `Community 1` be split into smaller, more focused modules?**
  _Cohesion score 0.11942959001782531 - nodes in this community are weakly interconnected._