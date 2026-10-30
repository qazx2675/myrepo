# Graph Report - ldap_check  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 15 nodes · 23 edges · 2 communities
- Extraction: 100% EXTRACTED · 0% INFERRED · 0% AMBIGUOUS
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `a99e0065`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- Community 0
- Community 1

## God Nodes (most connected - your core abstractions)
1. `ldap_check.sh script` - 6 edges
2. `test_check.sh script` - 5 edges
3. `conf_get()` - 3 edges
4. `conf_list()` - 2 edges
5. `ini_get()` - 2 edges
6. `report()` - 2 edges
7. `s4_has()` - 2 edges
8. `same_set()` - 2 edges
9. `check()` - 2 edges
10. `make_good()` - 2 edges

## Surprising Connections (you probably didn't know these)
- None detected - all connections are within the same source files.

## Import Cycles
- None detected.

## Communities (2 total, 0 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.39
Nodes (7): conf_get(), conf_list(), ini_get(), report(), s4_has(), same_set(), ldap_check.sh script

### Community 1 - "Community 1"
Cohesion: 0.60
Nodes (5): check(), make_good(), ng(), ok(), test_check.sh script

## Suggested Questions
_Not enough signal to generate questions. This usually means the corpus has no AMBIGUOUS edges, no bridge nodes, no INFERRED relationships, and all communities are tightly cohesive. Add more files or run with --mode deep to extract richer edges._