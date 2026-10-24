# Graph Report - safety_guard  (2026-10-23)

## Corpus Check
- Corpus is ~4,005 words - fits in a single context window. You may not need a graph.

## Summary
- 111 nodes · 239 edges · 11 communities (6 shown, 5 thin omitted)
- Extraction: 90% EXTRACTED · 10% INFERRED · 0% AMBIGUOUS · INFERRED: 25 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- Guard Unit Tests
- Reuse Prompt & Disclaimer
- RunGuard 5-Step Core
- Guard Stdlib Imports
- UI Colors & LineReader
- Pipe Scenario Script
- Demo Entrypoint
- Test Buffers
- TestMain
- Demo & Env Vars
- safetyguard Package

## God Nodes (most connected - your core abstractions)
1. `RunGuard()` - 23 edges
2. `runGuardStr()` - 12 edges
3. `ConfirmCode()` - 11 edges
4. `testOpts()` - 11 edges
5. `NewLineReader()` - 11 edges
6. `5-step safety guard` - 10 edges
7. `RunDemo()` - 9 edges
8. `LineReader` - 9 edges
9. `wantCancel()` - 8 edges
10. `Paint()` - 8 edges

## Surprising Connections (you probably didn't know these)
- `main()` --calls--> `RunDemo()`  [EXTRACTED]
  cmd/guarddemo/main.go → guard.go
- `main()` --calls--> `NewLineReader()`  [EXTRACTED]
  cmd/guarddemo/main.go → ui.go
- `NewCode generator override and ShowTargets` --references--> `NewCode()`  [EXTRACTED]
  README.md → guard.go
- `RunGuard()` --implements--> `5-step safety guard`  [EXTRACTED]
  guard.go → README.md
- `RunGuard()` --references--> `GuardOpts options`  [EXTRACTED]
  guard.go → README.md

## Import Cycles
- None detected.

## Hyperedges (group relationships)
- **Five-step guard sequence** — readme_step1_code, readme_step2_enter, readme_step3_type_names, readme_step4_yes, readme_step5_countdown [EXTRACTED 1.00]
- **safetyguard public API** — guard_runguard, guard_confirmcode, guard_newcode, ui_linereader, readme_guardopts [EXTRACTED 1.00]
- **Reuse prompt requirements** — prompt_placement_rules, prompt_special_target_confirm, prompt_audit_log_rule, prompt_verification_requirements, prompt_vendor_offline [INFERRED 0.85]

## Communities (11 total, 5 thin omitted)

### Community 0 - "Guard Unit Tests"
Cohesion: 0.21
Nodes (21): runCancel(), runGuardStr(), TestColorDisabled(), TestConfirmCode(), TestGuardCancel(), TestGuardCountdownLine(), TestGuardPass(), TestGuardShowTargets() (+13 more)

### Community 1 - "Reuse Prompt & Disclaimer"
Cohesion: 0.11
Nodes (21): NewCode(), Audit log without codes or passwords, Placement: RunGuard right before the dangerous action, exit 1 on cancel, Reuse prompt for adding safety guard to other Go tools, Copy package into project for offline build, Verification: vet, race tests, cancel-path tests, pipe scenarios, README disclaimer, Build and test (go vet, go test -race, 12 pipe scenarios), Exit codes ExitOK 0 / ExitFail 1 and cancel reasons (+13 more)

### Community 2 - "RunGuard 5-Step Core"
Cohesion: 0.26
Nodes (9): cancelGuard(), ConfirmCode(), RunDemo(), RunGuard(), secs(), Extra ConfirmCode for special targets, GuardOpts, lineMsg (+1 more)

### Community 3 - "Guard Stdlib Imports"
Cohesion: 0.18
Nodes (3): compareHosts(), contains(), GuardError

### Community 4 - "UI Colors & LineReader"
Cohesion: 0.36
Nodes (8): Bold(), ColorEnabled(), Cyan(), Green(), Paint(), Red(), RedBg(), Yellow()

### Community 5 - "Pipe Scenario Script"
Cohesion: 0.43
Nodes (6): check(), run(), SAFETYGUARD_COLOR, SAFETYGUARD_TEST_CODE, SAFETYGUARD_TEST_TICK_MS, guard_scenarios.sh script

## Knowledge Gaps
- **15 isolated node(s):** `safetyguard`, `SAFETYGUARD_COLOR`, `SAFETYGUARD_TEST_CODE`, `SAFETYGUARD_TEST_TICK_MS`, `Step 1: random 6-char code, then target list and count` (+10 more)
  These have ≤1 connection - possible missing edges or undocumented components. (Counts symbols only; 32 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **5 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `RunGuard()` connect `RunGuard 5-Step Core` to `Guard Unit Tests`, `Reuse Prompt & Disclaimer`, `Guard Stdlib Imports`, `UI Colors & LineReader`?**
  _High betweenness centrality (0.327) - this node is a cross-community bridge._
- **Are the 8 inferred relationships involving `RunGuard()` (e.g. with `.ReadLine()` and `.ReadLineTimeout()`) actually correct?**
  _`RunGuard()` has 8 INFERRED edges - model-reasoned connections that need verification._
- **What connects `safetyguard`, `SAFETYGUARD_COLOR`, `SAFETYGUARD_TEST_CODE` to the rest of the system?**
  _15 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Reuse Prompt & Disclaimer` be split into smaller, more focused modules?**
  _Cohesion score 0.10822510822510822 - nodes in this community are weakly interconnected._
- **Why does `5-step safety guard` connect `Reuse Prompt & Disclaimer` to `RunGuard 5-Step Core`?**
  _High betweenness centrality (0.132) - this node is a cross-community bridge._
- **Are the 2 inferred relationships involving `runGuardStr()` (e.g. with `RunGuard()` and `NewLineReader()`) actually correct?**
  _`runGuardStr()` has 2 INFERRED edges - model-reasoned connections that need verification._
- **Why does `runGuardStr()` connect `Guard Unit Tests` to `RunGuard 5-Step Core`?**
  _High betweenness centrality (0.075) - this node is a cross-community bridge._