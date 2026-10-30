# Graph Report - OS 환경설정 체크  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 38 nodes · 106 edges · 8 communities (6 shown, 2 thin omitted)
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

## God Nodes (most connected - your core abstractions)
1. `main()` - 26 edges
2. `green()` - 17 edges
3. `yellow()` - 9 edges
4. `report_ldap_info()` - 7 edges
5. `color_by_status()` - 6 edges
6. `red()` - 6 edges
7. `auto_record_done()` - 5 edges
8. `run_check_script()` - 5 edges
9. `build_check_target_list()` - 5 edges
10. `contains()` - 5 edges

## Surprising Connections (you probably didn't know these)
- `auto_record_done()` --calls--> `green()`  [EXTRACTED]
  os_check_final_annotated.sh → os_check_final_annotated.sh  _Bridges community 0 → community 1_
- `main()` --calls--> `build_check_target_list()`  [EXTRACTED]
  os_check_final_annotated.sh → os_check_final_annotated.sh  _Bridges community 0 → community 4_
- `main()` --calls--> `build_message_case6to9()`  [EXTRACTED]
  os_check_final_annotated.sh → os_check_final_annotated.sh  _Bridges community 0 → community 2_
- `main()` --calls--> `red()`  [EXTRACTED]
  os_check_final_annotated.sh → os_check_final_annotated.sh  _Bridges community 0 → community 3_
- `main()` --calls--> `report_dhcp_info()`  [EXTRACTED]
  os_check_final_annotated.sh → os_check_final_annotated.sh  _Bridges community 0 → community 7_

## Import Cycles
- None detected.

## Communities (8 total, 2 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.27
Nodes (10): auto_record_done(), auto_record_done_remote(), main(), report_check_res_summary(), report_kernel_by_infra(), report_kernel_info(), run_info_check(), select_user() (+2 more)

### Community 1 - "Community 1"
Cohesion: 0.43
Nodes (7): apply_extra_setting(), apply_os_setting(), green(), report_all_ok_summary(), run_check_script(), run_kernel_check(), run_post_apply_check()

### Community 2 - "Community 2"
Cohesion: 0.53
Nodes (4): build_message_case6to9(), check_pice_bios(), check_usb0_required(), print_horizontal()

### Community 3 - "Community 3"
Cohesion: 0.60
Nodes (5): color_by_status(), red(), report_ldap_info(), report_setting_check_fail(), report_splunk_info()

### Community 4 - "Community 4"
Cohesion: 0.67
Nodes (3): build_check_target_list(), contains(), filter_svrauto_targets()

### Community 5 - "Community 5"
Cohesion: 0.67
Nodes (3): is_prefix_host(), parse_pm_result(), run_os_check()

## Knowledge Gaps
- **2 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `main()` connect `Community 0` to `Community 1`, `Community 2`, `Community 3`, `Community 4`, `Community 5`, `Community 6`, `Community 7`?**
  _High betweenness centrality (0.191) - this node is a cross-community bridge._
- **Why does `green()` connect `Community 1` to `Community 0`, `Community 2`, `Community 3`, `Community 4`, `Community 5`?**
  _High betweenness centrality (0.060) - this node is a cross-community bridge._
- **Why does `yellow()` connect `Community 0` to `Community 1`, `Community 2`, `Community 3`, `Community 4`?**
  _High betweenness centrality (0.009) - this node is a cross-community bridge._