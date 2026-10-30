# Graph Report - awx_script  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 184 nodes · 579 edges · 13 communities
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
- Community 6
- Community 7
- Community 8
- Community 9
- Community 10
- Community 11
- Community 12

## God Nodes (most connected - your core abstractions)
1. `dns_cases()` - 26 edges
2. `01.AWX_nodeinfo_V2.sh script` - 22 edges
3. `t_eq()` - 22 edges
4. `case_begin()` - 19 edges
5. `case_end()` - 19 edges
6. `setup_case()` - 19 edges
7. `run_tests.sh script` - 18 edges
8. `err()` - 18 edges
9. `run01()` - 17 edges
10. `seed_raw()` - 17 edges

## Surprising Connections (you probably didn't know these)
- `01.AWX_nodeinfo_V2.sh script` --calls--> `02.source_dhcp_pxe.sh script`  [EXTRACTED]
  01.AWX_nodeinfo_V2.sh → 02.source_dhcp_pxe.sh

## Import Cycles
- None detected.

## Communities (13 total, 0 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.16
Nodes (32): add_tmp(), apply_infra_alias(), as_group_lines(), check_servers(), dhcp_info(), dns_ask(), dns_batch(), dns_cands() (+24 more)

### Community 1 - "Community 1"
Cohesion: 0.38
Nodes (28): auto_cases(), case1(), case2(), case34(), case5(), case6(), case7(), case9() (+20 more)

### Community 2 - "Community 2"
Cohesion: 0.13
Nodes (19): auto_dns_setup(), DA(), DA_ALL(), DA_REC(), DA_S(), DN(), dns_cases(), dns_ptr() (+11 more)

### Community 3 - "Community 3"
Cohesion: 0.27
Nodes (11): confirm(), disclaimer(), final_summary(), manifest_load(), _manifest_parse(), _manifest_reset(), _read_line(), _rollback() (+3 more)

### Community 4 - "Community 4"
Cohesion: 0.21
Nodes (14): ask_var(), _awk_target(), check_value(), _desc_out(), _effective(), guide_reset(), _host_reason(), _idx_of() (+6 more)

### Community 5 - "Community 5"
Cohesion: 0.17
Nodes (11): jobs, test, name, on, pull_request, push, name, run (+3 more)

### Community 6 - "Community 6"
Cohesion: 0.38
Nodes (10): check_partitions(), err(), map_infra(), os6_enabled(), os6_failed(), os6_run(), os_arg(), print_table() (+2 more)

### Community 7 - "Community 7"
Cohesion: 0.33
Nodes (11): _apply_changes(), backup_file(), check_targets(), err(), guard_hit(), lf_normalize(), on_int(), _target_parse() (+3 more)

### Community 8 - "Community 8"
Cohesion: 0.39
Nodes (4): enc_name(), load(), main(), read_name()

### Community 9 - "Community 9"
Cohesion: 0.32
Nodes (8): bad_usage(), count_empty(), _gus_selfcheck(), info(), list_empty(), setup_guide.sh script, ui_init(), usage()

### Community 10 - "Community 10"
Cohesion: 0.47
Nodes (6): case8(), fifo_alive(), fifo_finish(), fifo_start(), fifo_wait(), sig_case()

### Community 11 - "Community 11"
Cohesion: 0.33
Nodes (6): D12(), D3(), D6(), D6_FIXED(), D7(), mkln()

### Community 12 - "Community 12"
Cohesion: 0.67
Nodes (3): ok(), restore_latest(), run_setup_chain()

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `01.AWX_nodeinfo_V2.sh script` connect `Community 0` to `Community 6`?**
  _High betweenness centrality (0.026) - this node is a cross-community bridge._
- **Should `Community 2` be split into smaller, more focused modules?**
  _Cohesion score 0.12535612535612536 - nodes in this community are weakly interconnected._
- **Why does `02.source_dhcp_pxe.sh script` connect `Community 6` to `Community 0`?**
  _High betweenness centrality (0.021) - this node is a cross-community bridge._
- **Why does `dns_cases()` connect `Community 2` to `Community 1`?**
  _High betweenness centrality (0.011) - this node is a cross-community bridge._