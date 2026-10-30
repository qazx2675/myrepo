# Graph Report - config_check  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 27 nodes · 47 edges · 6 communities (5 shown, 1 thin omitted)
- Extraction: 100% EXTRACTED · 0% INFERRED · 0% AMBIGUOUS
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

## God Nodes (most connected - your core abstractions)
1. `do_check()` - 8 edges
2. `config_check.sh script` - 8 edges
3. `auto_record_done()` - 5 edges
4. `chk()` - 4 edges
5. `yellow()` - 4 edges
6. `report_fail()` - 4 edges
7. `run_auto_test.sh script` - 3 edges
8. `gsh()` - 3 edges
9. `report_status()` - 3 edges
10. `run_check()` - 3 edges

## Surprising Connections (you probably didn't know these)
- `do_check()` --calls--> `report_fail()`  [EXTRACTED]
  config_check.sh → config_check.sh  _Bridges community 1 → community 3_
- `do_check()` --calls--> `yellow()`  [EXTRACTED]
  config_check.sh → config_check.sh  _Bridges community 1 → community 2_
- `config_check.sh script` --calls--> `do_check()`  [EXTRACTED]
  config_check.sh → config_check.sh  _Bridges community 1 → community 4_
- `auto_record_done()` --calls--> `green()`  [EXTRACTED]
  config_check.sh → config_check.sh  _Bridges community 2 → community 3_
- `config_check.sh script` --calls--> `auto_record_done()`  [EXTRACTED]
  config_check.sh → config_check.sh  _Bridges community 2 → community 4_

## Import Cycles
- None detected.

## Communities (6 total, 1 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.33
Nodes (8): AUTO_SETUP_DIR, bad(), chk(), GOSSH_LOG, make_copy(), ok(), PATH, run_auto_test.sh script

### Community 1 - "Community 1"
Cohesion: 0.52
Nodes (6): do_check(), gsh(), report_combo(), report_status(), report_values(), run_check()

### Community 2 - "Community 2"
Cohesion: 0.67
Nodes (3): auto_record_done(), auto_record_done_remote(), yellow()

### Community 3 - "Community 3"
Cohesion: 0.67
Nodes (3): green(), red(), report_fail()

### Community 4 - "Community 4"
Cohesion: 0.67
Nodes (3): horiz(), lines(), config_check.sh script

## Knowledge Gaps
- **3 isolated node(s):** `AUTO_SETUP_DIR`, `GOSSH_LOG`, `PATH`
  These have ≤1 connection - possible missing edges. (Counts symbols only; 5 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **1 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `do_check()` connect `Community 1` to `Community 2`, `Community 3`, `Community 4`?**
  _High betweenness centrality (0.028) - this node is a cross-community bridge._
- **What connects `AUTO_SETUP_DIR`, `GOSSH_LOG`, `PATH` to the rest of the system?**
  _3 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Why does `config_check.sh script` connect `Community 4` to `Community 1`, `Community 2`?**
  _High betweenness centrality (0.026) - this node is a cross-community bridge._
- **Why does `auto_record_done()` connect `Community 2` to `Community 1`, `Community 3`, `Community 4`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._