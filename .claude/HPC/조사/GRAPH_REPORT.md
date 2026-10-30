# Graph Report - 조사  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 94 nodes · 179 edges · 10 communities (7 shown, 3 thin omitted)
- Extraction: 83% EXTRACTED · 17% INFERRED · 0% AMBIGUOUS · INFERRED: 31 edges (avg confidence: 0.85)
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

## God Nodes (most connected - your core abstractions)
1. `CollectInfra()` - 11 edges
2. `main()` - 11 edges
3. `Config` - 9 edges
4. `SurveyVMs()` - 9 edges
5. `Collect()` - 8 edges
6. `ApplStatus()` - 7 edges
7. `runGossh()` - 7 edges
8. `matchInfraLines()` - 6 edges
9. `LoadAsset()` - 5 edges
10. `applyInfraRegex()` - 4 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `ApplStatus()`  [INFERRED]
  survey/cmd/survey/main.go → survey/cmd/survey/rule.go
- `SurveyVMs()` --calls--> `ApplStatus()`  [INFERRED]
  survey/cmd/survey/vm.go → survey/cmd/survey/rule.go
- `CollectInfra()` --calls--> `firstNonEmptyLine()`  [INFERRED]
  survey/cmd/survey/collect.go → survey/cmd/survey/rule.go
- `CollectInfra()` --calls--> `mapInfraValue()`  [INFERRED]
  survey/cmd/survey/collect.go → survey/cmd/survey/rule.go
- `CollectInfra()` --calls--> `matchInfraLines()`  [INFERRED]
  survey/cmd/survey/collect.go → survey/cmd/survey/rule.go

## Import Cycles
- None detected.

## Communities (10 total, 3 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.17
Nodes (16): splitSDC(), ApplStatus(), applyInfraRegex(), firstNonEmptyLine(), mapInfraValue(), matchInfraLines(), squashSpace(), TestApplStatus() (+8 more)

### Community 1 - "Community 1"
Cohesion: 0.22
Nodes (15): Collect(), CollectInfra(), detectError(), extractConfigValue(), parsePdshLine(), runGossh(), splitLines(), main() (+7 more)

### Community 2 - "Community 2"
Cohesion: 0.16
Nodes (7): confPath(), normalizeField(), WriteTSV(), TestNormalizeField(), TestVMName(), dnsResolvable(), vmName()

### Community 3 - "Community 3"
Cohesion: 0.14
Nodes (13): defaults, runs-on, steps, run, jobs, build-test, name, on (+5 more)

### Community 4 - "Community 4"
Cohesion: 0.33
Nodes (3): LoadConfig(), splitKV(), MountRule

### Community 5 - "Community 5"
Cohesion: 0.47
Nodes (5): AssetRow, LoadAsset(), splitSpaceCols(), TestLoadAssetLocationWithSpaces(), TestSplitSpaceCols()

### Community 6 - "Community 6"
Cohesion: 0.60
Nodes (3): conf_get(), dbg(), run_survey.sh script

## Knowledge Gaps
- **1 isolated node(s):** `survey`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 18 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **3 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `CollectInfra()` connect `Community 1` to `Community 0`, `Community 2`?**
  _High betweenness centrality (0.063) - this node is a cross-community bridge._
- **Are the 6 inferred relationships involving `CollectInfra()` (e.g. with `normalizeField()` and `firstNonEmptyLine()`) actually correct?**
  _`CollectInfra()` has 6 INFERRED edges - model-reasoned connections that need verification._
- **What connects `survey` to the rest of the system?**
  _1 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 3` be split into smaller, more focused modules?**
  _Cohesion score 0.14285714285714285 - nodes in this community are weakly interconnected._
- **Why does `main()` connect `Community 1` to `Community 0`, `Community 2`, `Community 4`, `Community 5`?**
  _High betweenness centrality (0.061) - this node is a cross-community bridge._
- **Are the 8 inferred relationships involving `main()` (e.g. with `LoadAsset()` and `Collect()`) actually correct?**
  _`main()` has 8 INFERRED edges - model-reasoned connections that need verification._
- **Why does `SurveyVMs()` connect `Community 1` to `Community 0`, `Community 2`, `Community 5`?**
  _High betweenness centrality (0.035) - this node is a cross-community bridge._