# Graph Report - awxkit  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 146 nodes · 418 edges · 16 communities (6 shown, 10 thin omitted)
- Extraction: 96% EXTRACTED · 4% INFERRED · 0% AMBIGUOUS · INFERRED: 15 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a99e0065`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- Community 0
- Community 1
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

## God Nodes (most connected - your core abstractions)
1. `Client` - 59 edges
2. `Config` - 32 edges
3. `main()` - 13 edges
4. `LoadConfigAndClient()` - 12 edges
5. `main()` - 10 edges
6. `main()` - 10 edges
7. `main()` - 10 edges
8. `setup()` - 10 edges
9. `ResolvePath()` - 9 edges
10. `ResolveUser()` - 9 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `ReadHostList()`  [EXTRACTED]
  cmd/nodeinfo/main.go → config/config.go
- `LoadConfigAndClient()` --references--> `Config`  [EXTRACTED]
  cli/client.go → config/config.go
- `saveNodeInfoResult()` --references--> `Config`  [EXTRACTED]
  cmd/nodeinfo/main.go → config/config.go
- `LoadConfigAndClient()` --references--> `Client`  [EXTRACTED]
  cli/client.go → awx/client.go
- `PollInventoryUpdate()` --references--> `Client`  [EXTRACTED]
  cli/poll.go → awx/client.go

## Import Cycles
- None detected.

## Communities (16 total, 10 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.15
Nodes (25): NewClient(), contains(), ParseChoices(), ResolveChoice(), LoadConfigAndClient(), ColorStatus(), IsTerminalStatus(), PollInventoryUpdate() (+17 more)

### Community 1 - "Community 1"
Cohesion: 0.17
Nodes (16): AppendHistory(), Highlight(), main(), patchAndSync(), recoverSourceID(), reportSyncFail(), resolveSourceID(), fakeInput() (+8 more)

### Community 3 - "Community 3"
Cohesion: 0.12
Nodes (8): dhcp.sh script, doctor.sh script, invsync.sh script, ls.sh script, nodeinfo.sh script, pxe.sh script, setup.sh script, survey.sh script

### Community 4 - "Community 4"
Cohesion: 0.20
Nodes (5): Client, InventoryUpdate, inventorySourceUpdateResult, PingResult, checkConfiguredTemplate()

### Community 5 - "Community 5"
Cohesion: 0.62
Nodes (8): SetKey(), readFile(), TestSetKeyAppendWhenMissing(), TestSetKeyCRLF(), TestSetKeyEmptyValueAndTrailingComment(), TestSetKeyNoLeftoverTempAndMissingFile(), TestSetKeyReplace(), writeTemp()

### Community 9 - "Community 9"
Cohesion: 0.50
Nodes (3): Job, artifactKeys(), saveNodeInfoResult()

## Knowledge Gaps
- **1 isolated node(s):** `awxkit`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 24 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **10 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Client` connect `Community 4` to `Community 0`, `Community 1`, `Community 2`, `Community 6`, `Community 7`, `Community 8`, `Community 9`, `Community 10`, `Community 11`, `Community 12`, `Community 14`?**
  _High betweenness centrality (0.440) - this node is a cross-community bridge._
- **What connects `awxkit` to the rest of the system?**
  _1 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.1476923076923077 - nodes in this community are weakly interconnected._
- **Why does `Config` connect `Community 1` to `Community 0`, `Community 9`, `Community 2`?**
  _High betweenness centrality (0.088) - this node is a cross-community bridge._
- **Should `Community 3` be split into smaller, more focused modules?**
  _Cohesion score 0.125 - nodes in this community are weakly interconnected._
- **Why does `authTransport` connect `Community 8` to `Community 4`?**
  _High betweenness centrality (0.048) - this node is a cross-community bridge._