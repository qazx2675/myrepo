# Graph Report - 기타  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 1671 nodes · 4155 edges · 79 communities (52 shown, 27 thin omitted)
- Extraction: 97% EXTRACTED · 3% INFERRED · 0% AMBIGUOUS · INFERRED: 113 edges (avg confidence: 0.85)
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
- Community 29
- Community 30
- Community 31
- Community 32
- Community 33
- Community 34
- Community 35
- Community 36
- Community 37
- Community 38
- Community 39
- Community 40
- Community 41
- Community 42
- Community 43
- Community 44
- Community 45
- Community 46
- Community 47
- Community 48
- Community 49
- Community 50
- Community 51
- Community 52
- Community 53
- Community 54
- Community 55
- Community 56
- Community 57
- Community 59
- Community 60
- Community 61
- Community 62
- Community 63
- Community 64
- Community 65
- Community 67
- Community 68
- Community 69
- Community 70
- Community 71
- Community 72
- Community 73
- Community 74

## God Nodes (most connected - your core abstractions)
1. `Order` - 53 edges
2. `server` - 34 edges
3. `detect()` - 28 edges
4. `run()` - 26 edges
5. `BithumbClient` - 24 edges
6. `TestAnswerFormat` - 24 edges
7. `err()` - 23 edges
8. `err()` - 23 edges
9. `test_pilot.sh script` - 23 edges
10. `render()` - 22 edges

## Surprising Connections (you probably didn't know these)
- `GetDecrypted()` --calls--> `Decrypt()`  [INFERRED]
  개인프로젝트/home_lab/vm 생성 홈페이지 프로젝트/src/internal/secrets/store.go → 개인프로젝트/home_lab/vm 생성 홈페이지 프로젝트/src/internal/secrets/crypto.go
- `run()` --uses--> `Mode`  [INFERRED]
  개인프로젝트/코인자동매매/src/portfolio_backtest.py → 개인프로젝트/코인자동매매/src/risk.py
- `LiveData` --uses--> `BithumbClient`  [INFERRED]
  개인프로젝트/코인자동매매/src/live_loop.py → 개인프로젝트/코인자동매매/src/bithumb_client.py
- `LiveData` --uses--> `Order`  [INFERRED]
  개인프로젝트/코인자동매매/src/live_loop.py → 개인프로젝트/코인자동매매/src/fills.py
- `Trader` --uses--> `BithumbClient`  [INFERRED]
  개인프로젝트/코인자동매매/src/trader.py → 개인프로젝트/코인자동매매/src/bithumb_client.py

## Import Cycles
- None detected.

## Communities (79 total, 27 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.05
Nodes (103): _apply_changes(), ask_var(), _awk_target(), backup_file(), check_value(), confirm(), _desc_out(), _effective() (+95 more)

### Community 1 - "Community 1"
Cohesion: 0.05
Nodes (103): _apply_changes(), ask_var(), _awk_target(), backup_file(), check_value(), confirm(), _desc_out(), _effective() (+95 more)

### Community 2 - "Community 2"
Cohesion: 0.05
Nodes (49): _approval_topic(), approve(), clear_pending(), has_pending(), poll_phone_approvals(), propose(), _req_path(), should_propose() (+41 more)

### Community 3 - "Community 3"
Cohesion: 0.05
Nodes (53): body_of(), build_p1_prompt(), chat_once(), check_expect_warnings(), _clean_cell(), cmd_run(), cmd_summarize(), estimate_tokens() (+45 more)

### Community 4 - "Community 4"
Cohesion: 0.07
Nodes (11): App, ParsedMessage, MessageParser, AppDatabase, HistoryDao, HistoryEntity, GmailSender, Authenticator (+3 more)

### Community 5 - "Community 5"
Cohesion: 0.12
Nodes (47): _apply_changes(), ask_var(), _awk_target(), backup_file(), bad_usage(), check_targets(), check_value(), confirm() (+39 more)

### Community 6 - "Community 6"
Cohesion: 0.12
Nodes (47): _apply_changes(), ask_var(), _awk_target(), backup_file(), bad_usage(), check_targets(), check_value(), confirm() (+39 more)

### Community 7 - "Community 7"
Cohesion: 0.10
Nodes (23): Phase1Input, Phase2Input, Phase3Input, Phase4Input, Phase5Input, Phase6Input, Phase7Input, Phase8Input (+15 more)

### Community 8 - "Community 8"
Cohesion: 0.21
Nodes (12): server, baseData(), formInt(), logAudit(), render(), splitFields(), splitLines(), ClearSessionCookie() (+4 more)

### Community 9 - "Community 9"
Cohesion: 0.09
Nodes (22): _alias_in(), fetch_headlines(), _first_keyword(), Headline, poll(), scan(), Verdict, _cat_hit() (+14 more)

### Community 12 - "Community 12"
Cohesion: 0.10
Nodes (14): Result, Fill, compute(), _mdd(), Metrics, PFResult, Pos, _rows_for() (+6 more)

### Community 13 - "Community 13"
Cohesion: 0.10
Nodes (19): daily_loss_exceeded(), effective_fee_rate(), entry_krw(), fee_coupon_suspect(), get_mode(), Mode, must_liquidate(), stop_order() (+11 more)

### Community 14 - "Community 14"
Cohesion: 0.18
Nodes (27): aborted(), absent(), anc_case(), AUTO_SETUP_DIR, eq(), fail(), GOSSH_LOG, has() (+19 more)

### Community 15 - "Community 15"
Cohesion: 0.23
Nodes (19): Config, appendLog(), CreateJob(), Run(), setJobStatus(), RunPhase1(), RunPhase2(), RunPhase3() (+11 more)

### Community 16 - "Community 16"
Cohesion: 0.10
Nodes (8): LiveData, _config(), heartbeat(), notify(), live_enabled(), Trader, test_notifier_no_topic(), test_live_disabled_by_default()

### Community 17 - "Community 17"
Cohesion: 0.14
Nodes (13): Position, run(), save(), demo_ma_cross(), main(), _print(), main(), _period() (+5 more)

### Community 18 - "Community 18"
Cohesion: 0.16
Nodes (16): Order, execute_against_book(), execute_live(), PaperFill, strategy(), main(), _to_fill(), approx() (+8 more)

### Community 19 - "Community 19"
Cohesion: 0.12
Nodes (5): BithumbClient, load_keys(), _mask(), check_private(), check_public()

### Community 20 - "Community 20"
Cohesion: 0.11
Nodes (3): MainActivity, HomeScreen(), SettingsScreen()

### Community 22 - "Community 22"
Cohesion: 0.14
Nodes (3): read_jsonl(), TestBotComponents, TestUsers

### Community 23 - "Community 23"
Cohesion: 0.11
Nodes (10): ctxKey, Job, CreateSession(), DeleteSession(), userBySession(), WithUser(), Credential, User (+2 more)

### Community 24 - "Community 24"
Cohesion: 0.11
Nodes (19): ng(), ok(), scenarios.sh script, VC_PASSWORD, stop.sh script, timing.sh script, VC_PASSWORD, cleanup() (+11 more)

### Community 26 - "Community 26"
Cohesion: 0.13
Nodes (11): auditEntry, pipelineStep, bootstrapAdmin(), dirOf(), loadTemplates(), main(), randomPassword(), CheckPassword() (+3 more)

### Community 27 - "Community 27"
Cohesion: 0.30
Nodes (20): absent(), eq(), exists(), fail(), fe(), gen(), has(), hasnt() (+12 more)

### Community 29 - "Community 29"
Cohesion: 0.14
Nodes (10): gloss_question(), init_users(), _is_pure_citation(), load_state(), load_users(), save_state(), strip_clause_refs(), subscribe_stream() (+2 more)

### Community 31 - "Community 31"
Cohesion: 0.23
Nodes (12): detect(), is_bearish(), _ma_signal(), strategy(), _build(), test_ascending_triangle(), test_bear_flag(), test_box_range() (+4 more)

### Community 32 - "Community 32"
Cohesion: 0.28
Nodes (16): auto_record_done(), auto_record_done_remote(), do_check(), green(), gsh(), horiz(), lines(), read() (+8 more)

### Community 33 - "Community 33"
Cohesion: 0.34
Nodes (16): eq(), fail(), has(), hasnt(), isok(), jbak(), mkproj(), mkupd() (+8 more)

### Community 34 - "Community 34"
Cohesion: 0.16
Nodes (5): FakeResp, fake(), fake(), fake(), TestLLMCall

### Community 35 - "Community 35"
Cohesion: 0.24
Nodes (13): execute(), approx(), Bar, _df(), test_execution_delay(), strat(), test_limit_buy_fill_and_nofill(), test_market_buy() (+5 more)

### Community 36 - "Community 36"
Cohesion: 0.15
Nodes (8): base_score(), _breakout_score(), btc_regime_ok(), liquidity_ok(), _meanrev_score(), _momentum_score(), _rsi(), score()

### Community 37 - "Community 37"
Cohesion: 0.28
Nodes (15): auto_record_done(), auto_record_done_remote(), do_check(), green(), gsh(), horiz(), lines(), red() (+7 more)

### Community 39 - "Community 39"
Cohesion: 0.21
Nodes (11): _all_titles(), assemble_documents(), build_system_prompt(), estimate_tokens(), _full_tokens(), _history_tokens(), query_llm(), _query_terms() (+3 more)

### Community 40 - "Community 40"
Cohesion: 0.33
Nodes (9): Detection, is_bullish(), pattern_score(), combine(), _det(), test_bearish_veto(), test_bullish_detection_flows_through(), test_combine() (+1 more)

### Community 41 - "Community 41"
Cohesion: 0.31
Nodes (13): do_check(), green(), gsh(), horiz(), lines(), red(), report_combo(), report_fail() (+5 more)

### Community 42 - "Community 42"
Cohesion: 0.41
Nodes (12): eq(), fail(), has(), hasnt(), idx(), isno(), isok(), mkfix() (+4 more)

### Community 43 - "Community 43"
Cohesion: 0.16
Nodes (10): accept_question(), handle_feedback_only(), handle_question_only(), handle_single_topic_event(), is_own_feedback(), lookup_qa(), main(), make_user_handler() (+2 more)

### Community 46 - "Community 46"
Cohesion: 0.44
Nodes (11): eq(), fail(), has(), hasnt(), mkfix(), nocr(), noesc(), pass() (+3 more)

### Community 47 - "Community 47"
Cohesion: 0.23
Nodes (5): _classify(), _fit(), Line, _prior_trend(), _swings()

### Community 48 - "Community 48"
Cohesion: 0.23
Nodes (6): align(), load(), _path(), slice_period(), main(), _period()

### Community 49 - "Community 49"
Cohesion: 0.21
Nodes (11): add_turn(), build_answer_title(), cache_qa(), _ctx(), enqueue_question(), ensure_worker(), _first_line(), get_epoch() (+3 more)

### Community 50 - "Community 50"
Cohesion: 0.18
Nodes (9): append_jsonl(), _is_gloss_header(), now_iso(), parse_glossary(), process_feedback(), read_document(), read_document_raw(), refresh_documents() (+1 more)

### Community 52 - "Community 52"
Cohesion: 0.64
Nodes (9): absent(), eq(), fail(), has(), hasnt(), isok(), nocr(), pass() (+1 more)

### Community 53 - "Community 53"
Cohesion: 0.33
Nodes (8): AUTO_SETUP_DIR, bad(), chk(), GOSSH_LOG, make_copy(), ok(), PATH, run_auto_test.sh script

### Community 54 - "Community 54"
Cohesion: 0.31
Nodes (5): Get-Cloudflared(), Get-Ngrok(), Read-SharedText(), Start-Cloudflare(), Start-Ngrok()

### Community 55 - "Community 55"
Cohesion: 0.50
Nodes (7): build_header(), die2(), die3(), err(), info(), ok(), make_guide.sh script

### Community 56 - "Community 56"
Cohesion: 0.50
Nodes (7): build_header(), die2(), die3(), err(), info(), ok(), make_guide.sh script

### Community 60 - "Community 60"
Cohesion: 0.40
Nodes (4): GOVC_INSECURE, GOVC_URL, GOVC_USERNAME, fail_inject.sh script

### Community 61 - "Community 61"
Cohesion: 0.90
Nodes (4): running(), vcsim.sh script, start(), stop()

### Community 62 - "Community 62"
Cohesion: 0.40
Nodes (3): doc_paths(), startup_warmup(), _topic_text()

### Community 63 - "Community 63"
Cohesion: 0.50
Nodes (4): publish_ntfy(), _cap(), _nbytes(), _send_ntfy_payload()

### Community 64 - "Community 64"
Cohesion: 0.83
Nodes (3): CollapsedBubble(), OverlayBubbleContent(), PasteCard()

## Knowledge Gaps
- **20 isolated node(s):** `AUTO_SETUP_DIR`, `GOSSH_LOG`, `NO_COLOR`, `PATH`, `VC_PASSWORD` (+15 more)
  These have ≤1 connection - possible missing edges. (Counts symbols only; 363 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **27 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Order` connect `Community 18` to `Community 2`, `Community 35`, `Community 12`, `Community 13`, `Community 16`, `Community 17`, `Community 31`?**
  _High betweenness centrality (0.019) - this node is a cross-community bridge._
- **Are the 6 inferred relationships involving `Order` (e.g. with `Bot` and `LiveData`) actually correct?**
  _`Order` has 6 INFERRED edges - model-reasoned connections that need verification._
- **What connects `AUTO_SETUP_DIR`, `GOSSH_LOG`, `NO_COLOR` to the rest of the system?**
  _20 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.050118926265715256 - nodes in this community are weakly interconnected._
- **Why does `TestAnswerFormat` connect `Community 25` to `Community 45`, `Community 30`?**
  _High betweenness centrality (0.010) - this node is a cross-community bridge._
- **Should `Community 1` be split into smaller, more focused modules?**
  _Cohesion score 0.050118926265715256 - nodes in this community are weakly interconnected._
- **Why does `TestBotComponents` connect `Community 22` to `Community 45`, `Community 30`?**
  _High betweenness centrality (0.009) - this node is a cross-community bridge._