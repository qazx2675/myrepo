# Graph Report - HPC  (2026-10-13)

## Corpus Check
- Large corpus: 566 files · ~822,767 words. Semantic extraction will be expensive (many Claude tokens). Consider running on a subfolder.

## Summary
- 4219 nodes · 15014 edges · 169 communities (145 shown, 24 thin omitted)
- Extraction: 82% EXTRACTED · 18% INFERRED · 0% AMBIGUOUS · INFERRED: 2729 edges (avg confidence: 0.85)
- Token cost: 0 input · 0 output

## Community Hubs (Navigation)
- AWX 실행·설정 경로
- AWX 프로파일·완료 TUI
- redfish 모의 BMC·적용
- update_v0.4.0.sh 엔진
- update_v0.6.0.sh 엔진
- update_v0.7.0.sh 엔진
- update_v0.8.0.sh 엔진
- update_unreleased_dns.sh 엔진
- awx update_v0.6.0.sh 엔진
- awx update_v0.6.1.sh 엔진
- AWX 실행 테스트
- AWX 프로파일 파싱·테스트
- redfish 분류·집계
- awx_script 테스트 케이스
- TUI 메뉴·골든 테스트
- ip_change 엔진 출력
- 조사 도구 수집 구조
- 변경 화면 렌더링
- tty·시그널 유틸
- redfish 모의 BMC 테스트
- redfish 리포트 집계
- 데몬 단계 전이 테스트
- auto_setup 가이드 스크립트
- awx_script 가이드 스크립트
- 데몬·시계·job
- awxkit: choice.go
- redfish: attrCand
- redfish: .rowsFor()
- auto_setup: TestChangeHintStyleAndHelp()
- auto_setup: relayArgsOK()
- awx_script: 01.AWX_nodeinfo_V2.sh
- redfish: commonOpts
- redfish: .echo()
- auto_setup: change.go
- auto_setup: newChangeState()
- go_pkg_crypto_aes
- auto_setup: dnsAns
- redfish: hostOutcome
- ip_change: buildFrame()
- OS 환경설정 체크: os_check_final_annotated.sh
- auto_setup: TestParseCheckFQDNAndMismatc
- auto_setup: e2e_scen()
- redfish: cli
- auto_setup: Checker
- auto_setup: .key()
- awxkit: .CountInventoryHosts()
- ldap_setting: buildFrame()
- auto_setup: change_tui.go
- auto_setup: changeCmd
- auto_setup: TestChangeParseApplOut()
- auto_setup: lbEnv
- auto_setup: newhost.go
- auto_setup: realRunner
- auto_setup: .runWorkDraft()
- redfish: rtEnv
- copy: main()
- redfish: .writeMergedResults()
- auto_setup: .cancel()
- auto_setup: fakeReply
- redfish: Client
- auto_setup: newSource()
- auto_setup: readFull()
- auto_setup: work.go
- redfish: checkEnv
- auto_setup: noHostErr
- auto_setup: buildFailure
- auto_setup: validChangeUser()
- auto_setup: changeJob
- bytes.Buffer
- redfish: runDump()
- auto_setup: manual_check.sh
- auto_setup: auto_setup/check.go
- auto_setup: .refreshStages()
- auto_setup: runCode()
- auto_setup: envMap()
- auto_setup: demo_change.sh
- redfish: dumpRes
- auto_setup: CheckDNS()
- auto_setup: realOneoffStart()
- auto_setup: gunzipBytes()
- go_pkg_embed
- auto_setup: shortHost()
- auto_setup: auto_setup/client.go
- auto_setup: e2e_run_01()
- redfish: .fetch()
- auto_setup: anaconda_test.go
- auto_setup: change_tui_test.go
- auto_setup: NewRealChecker()
- auto_setup: .acceptManualRun()
- auto_setup: demo_flow.sh
- auto_setup: run_change_os.sh
- ldap_setting: asset/asset.go
- auto_setup: .doneFile()
- redfish: all_bios_check.sh
- auto_setup: Group
- auto_setup: remoteWorkStore
- awxkit: dhcp.sh
- redfish: Config
- config_check: config_check.sh
- auto_setup: .Set()
- redfish: checkOpts
- auto_setup: change_s6_test.go
- go_pkg_golang_org_x_crypto_s
- redfish: .AddHost()
- auto_setup: nhNormMAC()
- copy: .ClearDescription()
- redfish: link
- ldap_setting: .InfraNames()
- auto_setup: bgTask
- auto_setup: mkdir0700()
- auto_setup: fakeEncodeName()
- auto_setup: fakeZone
- auto_setup: inlineMenu
- ldap_setting: ldap_setting/internal/render
- ldap_setting: Load()
- redfish: FarmOptions
- auto_setup: done.go
- auto_setup: loadNewhostHistory()
- auto_setup: StageCounts
- awx_script: fake_dns.py
- config_check: run_auto_test.sh
- ldap_check: ldap_check.sh
- ldap_setting: lib_common.sh
- auto_setup: nopNotifier
- auto_setup: extractSecondTar()
- auto_setup: run_setup_scripts.sh
- ldap_setting: test_all.sh
- auto_setup: argAfter()
- auto_setup: newDefaultDaemon()
- auto_setup: realWorkScript()
- auto_setup: make_demo_jobs.sh
- awxkit: authTransport
- copy: send.sh
- ldap_check: test_check.sh
- auto_setup: workStore
- ip_change: ip_change/internal/render/ap
- 조사: run_survey.sh
- auto_setup: auto_setup/setup.sh
- auto_setup: tui_hook.go
- ip_change: ip_change/internal/render/ro
- ip_change: run_ip_change.sh
- ip_change: ip_change/setup.sh
- ldap_setting: ldap_setting/internal/render
- ldap_setting: deploy_ldap.sh
- ldap_setting: scripts/run.sh
- ldap_setting: ldap_setting/setup.sh
- redfish: redfish/setup.sh
- 패스워드변경자동화: 패스워드변경자동화/run.sh
- auto_setup: auto_setup/build_os6.sh
- auto_setup: parseWorkOS6Over()
- GPU 체크스크립트: check-gpu-power-limit.sh
- redfish: redfish/build_os6.sh
- 조사: survey/update.sh
- auto_setup: bgTask[T]
- auto_setup: fake_gossh.sh
- config_check: apply_auto_mode.sh
- ldap_setting: add_infra_from_ldapconf.sh
- ldap_setting: ldap_setting/update.sh
- redfish: build.sh
- redfish: mockbmc.sh
- 조사: infra_survey.sh
- auto_setup: auto_setup
- awxkit: awxkit
- redfish: biostool
- copy: copy-bridge
- ip_change: ip-change
- ldap_setting: ldap-automation
- 패스워드변경자동화: pwreset
- 조사: survey

## God Nodes (most connected - your core abstractions)
1. `tuiState` - 95 edges
2. `Daemon` - 93 edges
3. `startMockTree()` - 71 edges
4. `setupDir()` - 65 edges
5. `must()` - 63 edges
6. `twoGroupSnap()` - 59 edges
7. `changeState` - 52 edges
8. `Snapshot` - 52 edges
9. `newhostState` - 52 edges
10. `logf()` - 50 edges

## Surprising Connections (you probably didn't know these)
- `TestChangeKeyOffIsNoop()` --calls--> `wrap()`  [INFERRED]
  auto_setup/change_tui_test.go → ip_change/internal/color/color.go
- `titleLines()` --calls--> `headerUser()`  [INFERRED]
  auto_setup/render.go → auto_setup/sessionuser.go
- `queuedDaemon()` --calls--> `drive()`  [INFERRED]
  auto_setup/anaconda_test.go → auto_setup/daemon_test.go
- `queuedDaemon()` --calls--> `newTestDaemon()`  [INFERRED]
  auto_setup/anaconda_test.go → auto_setup/daemon_test.go
- `queuedDaemon()` --calls--> `newWorld()`  [INFERRED]
  auto_setup/anaconda_test.go → auto_setup/daemon_test.go

## Import Cycles
- None detected.

## Communities (169 total, 24 thin omitted)

### Community 0 - "AWX 실행·설정 경로"
Cohesion: 0.09
Nodes (8): blockReader, changeConfDir(), changeConfPath(), dumpMeta, APIError, attrErr, regAttr, validateAttr()

### Community 1 - "AWX 프로파일·완료 TUI"
Cohesion: 0.07
Nodes (17): AwxProfile, awxEnv(), awxState, doneState, keyEv, keyKind, newhostAwxEnv(), newhostAwxNote() (+9 more)

### Community 2 - "redfish 모의 BMC·적용"
Cohesion: 0.13
Nodes (67): applyRow, failReader, mockSnap, assertClean(), assertOneSession(), checkRCFor(), itemOf(), must() (+59 more)

### Community 3 - "update_v0.4.0.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 4 - "update_v0.6.0.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 5 - "update_v0.7.0.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 6 - "update_v0.8.0.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 7 - "update_unreleased_dns.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 8 - "awx update_v0.6.0.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 9 - "awx update_v0.6.1.sh 엔진"
Cohesion: 0.08
Nodes (72): _abs_path(), _add_secval(), _anchor_lines(), _anchor_report(), _apply_change(), _apply_changes(), ask_var(), _awk_target() (+64 more)

### Community 10 - "AWX 실행 테스트"
Cohesion: 0.08
Nodes (62): awxCall, TestAwxEscCancelsPrompt(), TestAwxInterruptedMessage(), TestAwxListSkipKeepsFile(), TestAwxListWrittenAndEnv(), TestAwxMissingDirOrScript(), TestAwxNoProfilesNoPrompt(), TestAwxPausesKeyReader() (+54 more)

### Community 11 - "AWX 프로파일 파싱·테스트"
Cohesion: 0.06
Nodes (64): parseAwxProfiles(), setProfiles(), TestConfVarsHasAwxProfiles(), TestParseAwxProfiles(), TestParseAwxProfilesNine(), TestAwxLayoutNoEllipsis(), TestAwxLayoutWideAndShort(), TestParseAwxList() (+56 more)

### Community 12 - "redfish 분류·집계"
Cohesion: 0.08
Nodes (49): allEnv, allHost, allOpts, allRun, allScenario, attrDiffRow, hostNote, ignoreSet (+41 more)

### Community 13 - "awx_script 테스트 케이스"
Cohesion: 0.13
Nodes (59): auto_cases(), auto_dns_setup(), case1(), case2(), case34(), case5(), case6(), case7() (+51 more)

### Community 14 - "TUI 메뉴·골든 테스트"
Cohesion: 0.09
Nodes (53): TestChangeMenuInlineGolden(), TestInlineMenuOpenCloseByteIdentical(), grpRef, TestMergedGroup(), TestMenuHintLines(), TestHintSegsColors(), barSegs(), clockOf() (+45 more)

### Community 15 - "ip_change 엔진 출력"
Cohesion: 0.08
Nodes (45): formatResultLine(), formatRollbackLine(), indent(), ipMonitorLabel(), main(), run(), runRollback(), TestFormatResultLineFail() (+37 more)

### Community 16 - "조사 도구 수집 구조"
Cohesion: 0.07
Nodes (46): AssetRow, CollectResult, Config, InfraResult, MountRule, LoadAsset(), splitSpaceCols(), Collect() (+38 more)

### Community 17 - "변경 화면 렌더링"
Cohesion: 0.12
Nodes (39): atom, renderAwx(), countLine(), padTo(), rsLabel(), RenderDNSTable(), renderFlow(), nhPad() (+31 more)

### Community 18 - "tty·시그널 유틸"
Cohesion: 0.06
Nodes (35): ioctl(), isTTY(), killGroup(), makeRaw(), notifyResize(), oneoffSysProcAttr(), ttySize(), waitReadable() (+27 more)

### Community 19 - "redfish 모의 BMC 테스트"
Cohesion: 0.10
Nodes (42): mockBMC, mockCounts, sleepRec, NewClient(), connStateCounter(), TestSecAttributesNotSent(), TestSecAttributesValidation(), TestSecAuthModePrivate() (+34 more)

### Community 20 - "redfish 리포트 집계"
Cohesion: 0.12
Nodes (42): aggEntry, allGroup, allRef, allReportOpts, failItem, itemResult, noteGroup, painter (+34 more)

### Community 21 - "데몬 단계 전이 테스트"
Cohesion: 0.21
Nodes (47): TestRecheckAnacondaClosedJobKeepsStage(), hostStage(), TestDaemonStuckThenReady(), TestSecondStageVisible(), TestStageTransitionsAndLdap(), jobFile(), doneJob(), drive() (+39 more)

### Community 22 - "auto_setup 가이드 스크립트"
Cohesion: 0.12
Nodes (47): _apply_changes(), ask_var(), _awk_target(), backup_file(), bad_usage(), check_targets(), check_value(), confirm() (+39 more)

### Community 23 - "awx_script 가이드 스크립트"
Cohesion: 0.12
Nodes (47): _apply_changes(), ask_var(), _awk_target(), backup_file(), bad_usage(), check_targets(), check_value(), confirm() (+39 more)

### Community 24 - "데몬·시계·job"
Cohesion: 0.13
Nodes (14): Clock, Daemon, active(), cloneJob(), pickIP(), selectSecondTargets(), unreachableLine(), wallMessage() (+6 more)

### Community 25 - "awxkit: choice.go"
Cohesion: 0.13
Nodes (33): contains(), ParseChoices(), ResolveChoice(), AppendHistory(), LoadConfigAndClient(), ColorStatus(), IsTerminalStatus(), PollInventoryUpdate() (+25 more)

### Community 26 - "redfish: attrCand"
Cohesion: 0.09
Nodes (36): attrCand, attrRegistry, getFunc, hostSummary, odataLink, regAttr, RFError, stdKeyword (+28 more)

### Community 27 - "redfish: .rowsFor()"
Cohesion: 0.09
Nodes (29): profRow, setEnv, setHost, setItem, setPlan, errBrief(), errStatus(), normalizeModel() (+21 more)

### Community 28 - "auto_setup: TestChangeHintStyleAndHelp()"
Cohesion: 0.15
Nodes (39): TestChangeHintStyleAndHelp(), TestChangeHintWrapsNoTruncation(), errAfter, fakeSrc, TestMenuHintAndMenuLinesWrap(), flatGroups(), stripANSI(), allRenders() (+31 more)

### Community 29 - "auto_setup: relayArgsOK()"
Cohesion: 0.07
Nodes (39): relayArgsOK(), requestArgsOK(), TestRelayArgsOK(), startFakeDaemon(), TestPidFileAndStopDaemon(), TestStopDaemonTimeoutNoKill(), isRunning(), pidPath() (+31 more)

### Community 30 - "awx_script: 01.AWX_nodeinfo_V2.sh"
Cohesion: 0.11
Nodes (42): add_tmp(), apply_infra_alias(), as_group_lines(), check_servers(), dhcp_info(), dns_ask(), dns_batch(), dns_cands() (+34 more)

### Community 31 - "redfish: commonOpts"
Cohesion: 0.08
Nodes (39): commonOpts, notImplementedError, runContext, cmdAllCheck(), indexDumpHosts(), TestIndexDumpHosts(), cmdDump(), TestCmdDumpRejectsFromDump() (+31 more)

### Community 32 - "redfish: .echo()"
Cohesion: 0.11
Nodes (11): Call, Host, Job, session, statusWriter, isLogPath(), mustJSON(), normPath() (+3 more)

### Community 33 - "auto_setup: change.go"
Cohesion: 0.08
Nodes (37): assetSites(), changeMenuItems(), changeTracked(), clip(), ipGateway(), ldapInfraNames(), lineNo(), loadSecretMask() (+29 more)

### Community 34 - "auto_setup: newChangeState()"
Cohesion: 0.18
Nodes (37): newChangeState(), renderChange(), TestChangeCancelDuringRun(), TestChangeDryRunCancelBackToInput(), TestChangeInfraMenu(), TestChangeInputCancelAndEmpty(), TestChangeLDAPDryRunPreview(), TestChangeMissingEngineFatal() (+29 more)

### Community 35 - "go_pkg_crypto_aes"
Cohesion: 0.10
Nodes (31): Result, decrypt(), encrypt(), encryptFromReader(), loadKey(), loadOrCreateKey(), newGCM(), readPassword() (+23 more)

### Community 36 - "auto_setup: dnsAns"
Cohesion: 0.11
Nodes (29): dnsAns, ApplyDNS(), dnsAddWarnings(), dnsBuildQuery(), dnsCandidates(), dnsExchange(), dnsJudge(), dnsParse() (+21 more)

### Community 37 - "redfish: hostOutcome"
Cohesion: 0.14
Nodes (35): hostOutcome, resSampler, TestAllCheckAuthStop(), TestAllCheckConcurrentFarm(), doCheck(), fastCheck(), mib(), openFilesLimit() (+27 more)

### Community 38 - "ip_change: buildFrame()"
Cohesion: 0.09
Nodes (24): buildFrame(), detailLines(), fmtDuration(), counts, hostState, Label, Monitor, row (+16 more)

### Community 39 - "OS 환경설정 체크: os_check_final_annotated.sh"
Cohesion: 0.15
Nodes (36): apply_extra_setting(), apply_os_setting(), auto_record_done(), auto_record_done_remote(), build_check_target_list(), build_message_case6to9(), check_ev_extra(), check_pice_bios() (+28 more)

### Community 40 - "auto_setup: TestParseCheckFQDNAndMismatc"
Cohesion: 0.14
Nodes (33): TestParseCheckFQDNAndMismatch(), newX(), newXDaemon(), secondScenario(), TestEffectiveStageStuckBoundary(), TestRestartResetsTransient(), TestSecondCheckMerge(), TestSecondSkippedWhenUnset() (+25 more)

### Community 41 - "auto_setup: e2e_scen()"
Cohesion: 0.18
Nodes (29): e2e_scen(), AUTO_SETUP_DIR, bad(), cleanup(), cmp_lr(), dalive(), dkill9(), dpid() (+21 more)

### Community 42 - "redfish: cli"
Cohesion: 0.19
Nodes (31): cli, main(), New(), collectLinks(), mustWrite(), newCli(), newSrv(), patchBody() (+23 more)

### Community 43 - "auto_setup: Checker"
Cohesion: 0.11
Nodes (16): Checker, fakeLdap, Runner, Second, LdapBackup, LdapState, nopLdap, nopSecond (+8 more)

### Community 44 - "auto_setup: .key()"
Cohesion: 0.18
Nodes (7): nhCheckDups(), newNewhostState(), rowNums(), newhostGoldenState(), TestNewhostGolden(), newhostState, nhVals

### Community 45 - "awxkit: .CountInventoryHosts()"
Cohesion: 0.10
Nodes (16): Host, hostListResult, InventorySource, inventorySourceListResult, inventorySourceUpdateResult, LaunchResult, PingResult, SurveyQuestion (+8 more)

### Community 46 - "ldap_setting: buildFrame()"
Cohesion: 0.10
Nodes (22): buildFrame(), detailLines(), fmtDuration(), counts, hostState, Label, Monitor, row (+14 more)

### Community 47 - "auto_setup: change_tui.go"
Cohesion: 0.09
Nodes (22): changeItemDesc(), changeMenuLines(), TestChangeMenuLinesWrapNoTruncation(), TestSetupModifyIsRed(), fitBody(), renderOneoff(), tailLines(), TestOneoffLayoutHostEchoSameAsPacking() (+14 more)

### Community 48 - "auto_setup: changeCmd"
Cohesion: 0.15
Nodes (21): changeCmd, changeOut, chgOS6WrapperScript(), emptyGossh(), engineErr(), failMap(), hostSet(), ipEngineArgs() (+13 more)

### Community 49 - "auto_setup: TestChangeParseApplOut()"
Cohesion: 0.14
Nodes (30): TestChangeParseApplOut(), lbEntry, lbInfo, applyFail(), binddnUID(), buildRemoteCmd(), collectScript(), detectScript() (+22 more)

### Community 50 - "auto_setup: lbEnv"
Cohesion: 0.26
Nodes (28): lbEnv, init(), NewRealLdapBackup(), decodeCmd(), lbBackup(), lbConf(), lbConfU(), lbJob() (+20 more)

### Community 51 - "auto_setup: newhost.go"
Cohesion: 0.15
Nodes (29): addNewhostTemplate(), loadNewhostTemplates(), newhostSave(), newhostUserDir(), nhCheckCol(), nhColName(), nhComplete(), nhFindTpl() (+21 more)

### Community 52 - "auto_setup: realRunner"
Cohesion: 0.14
Nodes (28): realRunner, buildCodeText(), buildCodeTextMode(), checkModeUnsupported(), extractSummaries(), lineHost(), modeLabel(), noReportDiag() (+20 more)

### Community 53 - "auto_setup: .runWorkDraft()"
Cohesion: 0.17
Nodes (6): newWorkState(), goldenWorkState(), workNotes(), workDraft, workNote, workState

### Community 54 - "redfish: rtEnv"
Cohesion: 0.19
Nodes (24): rtEnv, checkRowsState(), containsLine(), mustRead(), newRTEnv(), rtAll(), rtCapture(), rtCol() (+16 more)

### Community 55 - "copy: main()"
Cohesion: 0.13
Nodes (27): main(), run(), abs32(), getCursorPos(), getWindowOrigin(), handleFetch(), onPaint(), setClipboardText() (+19 more)

### Community 56 - "redfish: .writeMergedResults()"
Cohesion: 0.14
Nodes (22): checkRun, hostState, retryMerge, tsvTable, isRetryStatus(), sortedKeys(), writeLines(), writeResultFiles() (+14 more)

### Community 58 - "auto_setup: fakeReply"
Cohesion: 0.12
Nodes (19): fakeReply, buildEchoRequest(), icmpChecksum(), mergeBoth(), newIcmpPinger(), NewRealPinger(), parseEchoReply(), TestBuildParseEcho() (+11 more)

### Community 59 - "redfish: Client"
Cohesion: 0.14
Nodes (14): Client, ClientOpts, credentials, Mode, newDumpFileGet(), checkAllowed(), checkBody(), deniedGetSeg() (+6 more)

### Community 60 - "auto_setup: newSource()"
Cohesion: 0.15
Nodes (25): newSource(), snapshotNow(), cmdDaemonCtl(), cmdDone(), cmdReport(), cmdRequest(), cmdSnapshot(), ctlIsTTY() (+17 more)

### Community 61 - "auto_setup: readFull()"
Cohesion: 0.09
Nodes (8): readFull(), countConn, farmListener, FarmTotals, canonIP(), Farm, SessionStats, selfSignedCert()

### Community 62 - "auto_setup: work.go"
Cohesion: 0.15
Nodes (26): commaN(), judgeWork(), TestJudgeWork(), TestWorkRetryHostsAndStatus(), workConnFail(), workExecute(), workGosshBin(), workHostStatus() (+18 more)

### Community 63 - "redfish: checkEnv"
Cohesion: 0.14
Nodes (15): checkEnv, hostCheck, memoGet, memoRes, mmDumper, regCache, regEntry, dellBiosJob() (+7 more)

### Community 64 - "auto_setup: noHostErr"
Cohesion: 0.12
Nodes (16): noHostErr, os6Sessions, parseProbeLines(), probeLoop(), readProbeList(), shQuote(), newTestSessions(), TestOS6SessionsDeadIsUnknownAndRestarts() (+8 more)

### Community 65 - "auto_setup: buildFailure"
Cohesion: 0.19
Nodes (22): buildFailure, buildBins(), prepState(), runBin(), stubEnv(), TestMain(), TestRemoteDoneAndRequest(), TestRemoteEmptyState() (+14 more)

### Community 66 - "auto_setup: validChangeUser()"
Cohesion: 0.18
Nodes (23): validChangeUser(), TestNewhostCLI(), parseUserRoute(), pickUser(), TestParseUserRoute(), userMenu(), askUserCLI(), fixedUser() (+15 more)

### Community 67 - "auto_setup: changeJob"
Cohesion: 0.18
Nodes (17): changeJob, changePlan, changeAutofill(), changeLogSummary(), changeUserDir(), createChangeRecord(), hostOutcome(), isApplyKind() (+9 more)

### Community 68 - "bytes.Buffer"
Cohesion: 0.16
Nodes (19): opts, indent(), loadTargets(), main(), modeLabel(), run(), runRollback(), BuildCommand() (+11 more)

### Community 69 - "redfish: runDump()"
Cohesion: 0.30
Nodes (23): runDump(), copyTree(), dumpRC(), fastDump(), keys(), listFiles(), summaryText(), TestDumpAllTrees() (+15 more)

### Community 70 - "auto_setup: manual_check.sh"
Cohesion: 0.17
Nodes (20): AUTO_SETUP_DIR, calive(), checkpoint(), cli(), cpid(), ctl(), daemon_count(), daemon_pids() (+12 more)

### Community 71 - "auto_setup: auto_setup/check.go"
Cohesion: 0.19
Nodes (19): checkLocal(), checkOS6(), cleanupTemp(), clipWhy(), diagHead(), hostList(), logGosshUnparsed(), markTimeout() (+11 more)

### Community 72 - "auto_setup: .refreshStages()"
Cohesion: 0.19
Nodes (15): secs(), GroupView, baseStage(), buildSnapJob(), effectiveStage(), groupHosts(), hostNote(), jobGroups() (+7 more)

### Community 73 - "auto_setup: runCode()"
Cohesion: 0.22
Nodes (21): runCode(), TestRunnerCheckOnlyMode(), fileExists(), NewRealRunner(), needBash(), TestRunnerRunEndToEnd(), init(), NewRealSecond() (+13 more)

### Community 74 - "auto_setup: envMap()"
Cohesion: 0.29
Nodes (16): envMap(), nhFakeDNS(), nhSetup(), readLog(), TestNewhostAssignLanguage(), TestNewhostAwxProfiles(), TestNewhostBigPaste(), TestNewhostCancel() (+8 more)

### Community 75 - "auto_setup: demo_change.sh"
Cohesion: 0.14
Nodes (18): build_engine(), CGO_ENABLED, E2E_SCEN, GOPROXY, demo_change.sh script, ts(), chg_fx_host(), chg_make_nodes() (+10 more)

### Community 76 - "redfish: dumpRes"
Cohesion: 0.19
Nodes (13): dumpRes, dumpSession, dirPart(), dumpWith(), hostRelDir(), lastSeg(), membersCount(), newDumpSession() (+5 more)

### Community 77 - "auto_setup: CheckDNS()"
Cohesion: 0.30
Nodes (18): CheckDNS(), DNSMismatchHosts(), byHost(), newFakeDNS(), nsLines(), ptrName(), TestCheckDNSAllFailDisabled(), TestCheckDNSDuplicateWarnings() (+10 more)

### Community 78 - "auto_setup: realOneoffStart()"
Cohesion: 0.14
Nodes (14): realOneoffStart(), oneoffProc, dhcpCandidates(), dhcpLinkSh(), firstExisting(), os6OSCheckPath(), setPathVars(), TestDhcpCandidates() (+6 more)

### Community 79 - "auto_setup: gunzipBytes()"
Cohesion: 0.20
Nodes (19): gunzipBytes(), gzipBytes(), fakeGosshRun(), gosshOutput(), TestWorkCapGz(), TestWorkCleanOut(), TestWorkCleanTmp(), TestWorkCLI() (+11 more)

### Community 80 - "go_pkg_embed"
Cohesion: 0.20
Nodes (17): ApplyScript(), boolVar(), RollbackMode, hasService(), RollbackScript(), infra(), TestApplyScriptEmbedsValues(), TestApplyScriptIncludesLib() (+9 more)

### Community 81 - "auto_setup: shortHost()"
Cohesion: 0.15
Nodes (15): shortHost(), TestWorkParseHosts(), TestWorkSameListReuse(), workPrepareTmp(), workCleanTmp(), workExpand(), workListDir(), workMatchHost() (+7 more)

### Community 82 - "auto_setup: auto_setup/client.go"
Cohesion: 0.17
Nodes (12): parseRemote(), relayRemote(), remoteBin(), remoteCommand(), remoteFail(), remoteRun(), requestArgs(), TestParseRemote() (+4 more)

### Community 83 - "auto_setup: e2e_run_01()"
Cohesion: 0.20
Nodes (14): e2e_run_01(), e2e_run_oc(), AUTO_SETUP_DIR, bad(), daemon_count(), daemon_pids(), ok(), PATH (+6 more)

### Community 84 - "redfish: .fetch()"
Cohesion: 0.20
Nodes (13): biosSnap, CallStats, dumpOpts, dumpResult, Target, skippedSnap(), appendIndex(), candText() (+5 more)

### Community 85 - "auto_setup: anaconda_test.go"
Cohesion: 0.19
Nodes (7): queuedDaemon(), TestApplyCheckAnacondaWithoutSeenDown(), TestApplyCheckNormalResponseKeepsQueued(), TestRecheckAnacondaShowsStageChange(), fakeClock, simHost, world

### Community 86 - "auto_setup: change_tui_test.go"
Cohesion: 0.28
Nodes (13): chgTUISetup(), chgTUI, TestChangeKeyOffIsNoop(), TestChangeKeyOnShowsHintAndMenu(), TestChangeKeyRemoteClient(), TestChangeMenuInlineSurvivesRefresh(), TestChangeMenuSelectFlowBoth(), TestChangePreviewWarnsTrackedHost() (+5 more)

### Community 87 - "auto_setup: NewRealChecker()"
Cohesion: 0.21
Nodes (17): NewRealChecker(), fakeCheckBin(), fakeExternal(), TestCheckLocal(), TestCheckLocalExecFailure(), TestCheckOS6(), TestCheckOS6BadOutput(), TestCheckOS6ErrorShowsReason() (+9 more)

### Community 88 - "auto_setup: .acceptManualRun()"
Cohesion: 0.20
Nodes (9): listReqFiles(), parseRequest(), readActiveRequests(), recheckDir(), rejectRequest(), reqActiveDir(), writeRecheck(), SnapManual (+1 more)

### Community 89 - "auto_setup: demo_flow.sh"
Cohesion: 0.20
Nodes (15): E2E_SCEN, demo_flow.sh script, ts(), e2e_build(), e2e_build_variants(), e2e_make_oc(), e2e_make_oc_var(), e2e_make_rgossh() (+7 more)

### Community 90 - "auto_setup: run_change_os.sh"
Cohesion: 0.18
Nodes (13): build(), CGO_ENABLED, chk(), chkn(), FAKE_UNGUARD, GOPROXY, new_world(), newip() (+5 more)

### Community 91 - "ldap_setting: asset/asset.go"
Cohesion: 0.24
Nodes (16): Entry, GroupBySite(), Load(), LoadHostList(), StripEV(), TestDuplicateHostLastWins(), TestGroupBySite(), TestLoadHostList() (+8 more)

### Community 92 - "auto_setup: .doneFile()"
Cohesion: 0.27
Nodes (12): doneSnap(), doneTUI(), chgTUI, squash(), TestDoneHelpAndOverviewUnaffected(), TestDoneHintAndPromptWidths(), TestDoneKeyAllViewAndNoTargets(), TestDoneKeyCancelAndIgnore() (+4 more)

### Community 93 - "redfish: all_bios_check.sh"
Cohesion: 0.21
Nodes (12): opt_val(), all_bios_check.sh script, usage(), opt_val(), bios_check.sh script, usage(), encrypt.sh script, conf_val() (+4 more)

### Community 94 - "auto_setup: Group"
Cohesion: 0.25
Nodes (15): Group, Host, Job, jobSpec, runStatus(), readJobsIn(), TestParseJobSpecGroups(), formatJobStatus() (+7 more)

### Community 95 - "auto_setup: remoteWorkStore"
Cohesion: 0.27
Nodes (10): remoteWorkStore, cmdWorkRelay(), relayInProcess(), TestWkEncNoDangerWords(), TestWorkDanger(), TestWorkRelayRoundTrip(), wkDec(), wkEnc() (+2 more)

### Community 96 - "awxkit: dhcp.sh"
Cohesion: 0.12
Nodes (8): dhcp.sh script, doctor.sh script, invsync.sh script, ls.sh script, nodeinfo.sh script, pxe.sh script, setup.sh script, survey.sh script

### Community 97 - "redfish: Config"
Cohesion: 0.24
Nodes (14): Config, defaultConfig(), loadConf(), parseConf(), parseIntAtLeast(), setIfNotEmpty(), TestLoadConf(), TestParseConfAllKeys() (+6 more)

### Community 98 - "config_check: config_check.sh"
Cohesion: 0.28
Nodes (15): auto_record_done(), auto_record_done_remote(), do_check(), green(), gsh(), horiz(), lines(), red() (+7 more)

### Community 99 - "auto_setup: .Set()"
Cohesion: 0.19
Nodes (5): fakeConn, newFakeConn(), workCleanAll(), workCleanOut(), newRunDir()

### Community 100 - "redfish: checkOpts"
Cohesion: 0.24
Nodes (14): checkOpts, Profile, listProfileNames(), loadProfile(), parseProfile(), mustProfile(), tabs(), TestParseProfileErrors() (+6 more)

### Community 101 - "auto_setup: change_s6_test.go"
Cohesion: 0.36
Nodes (11): s6Copy(), s6DirsEqual(), s6Glob(), s6Has(), s6Read(), s6Run(), s6Wait(), TestChangeLabSelf() (+3 more)

### Community 102 - "go_pkg_golang_org_x_crypto_s"
Cohesion: 0.28
Nodes (6): fsmState, promptFSM, matchShellPrompt(), newPromptFSM(), runShell(), tryCandidate()

### Community 103 - "redfish: .AddHost()"
Cohesion: 0.22
Nodes (9): faultRule, HostOptions, scaleTreeFor(), Fault, Options, Tree, LoadTree(), loadTree() (+1 more)

### Community 104 - "auto_setup: nhNormMAC()"
Cohesion: 0.18
Nodes (11): nhNormMAC(), nhNumRange(), nhParseLine(), nhParseRowCmd(), nhTargets(), nhRowsOf(), TestNhCheckDups(), TestNhParseLine() (+3 more)

### Community 105 - "copy: .ClearDescription()"
Cohesion: 0.30
Nodes (3): Client, JobTemplate, jobTemplateList

### Community 106 - "redfish: link"
Cohesion: 0.26
Nodes (10): link, mockClient(), TestMockAuthFailStopsImmediately(), TestMockBasicFallbackWhenNoSessionService(), TestMockClientNeverReachesLogPaths(), TestMockDroppedConnectionIsUnreachable(), TestMockReadOnlyFlowAllTrees(), TestMockSetModeWritesPendingAndJob() (+2 more)

### Community 107 - "ldap_setting: .InfraNames()"
Cohesion: 0.32
Nodes (7): Site, build(), buildInfra(), Config, Infra, S4Rule, splitList()

### Community 108 - "auto_setup: bgTask"
Cohesion: 0.22
Nodes (6): bgTask, startBG(), flowFrame, flowScroll, pasteBuf, pasteEv

### Community 109 - "auto_setup: mkdir0700()"
Cohesion: 0.31
Nodes (3): mkdir0700(), localWorkStore, workWriteFile()

### Community 110 - "auto_setup: fakeEncodeName()"
Cohesion: 0.29
Nodes (4): fakeEncodeName(), fakeParseQ(), fakeAns, fakeDNS

### Community 111 - "auto_setup: fakeZone"
Cohesion: 0.18
Nodes (5): fakeZone, gateIn, slowReader, NewClient(), backoffFor()

### Community 112 - "auto_setup: inlineMenu"
Cohesion: 0.36
Nodes (9): inlineMenu, awxMenu(), hostKey(), hostMenu(), lowerKey(), menuLines(), TestMenuInlineHeight(), TestMenusGolden() (+1 more)

### Community 113 - "ldap_setting: ldap_setting/internal/render"
Cohesion: 0.40
Nodes (9): apply_auto_appl(), apply_autofs_auth(), commit_file(), set_directive(), set_ini_in_section(), set_kv(), set_line_list(), apply_body.sh script (+1 more)

### Community 114 - "ldap_setting: Load()"
Cohesion: 0.49
Nodes (9): Load(), TestBadLineRejected(), TestDefaultSiteParsed(), TestDuplicateStorageRejected(), TestLoadGood(), TestMissingKeyRejected(), TestUnknownURIKeyRejected(), TestURIListSkipsNONE() (+1 more)

### Community 115 - "redfish: FarmOptions"
Cohesion: 0.42
Nodes (9): FarmOptions, NewFarm(), farmCli(), mustTree(), newFarmT(), TestFarmPerHostCredsFaultsAndConnCounts(), TestFarmSeparatesHostsByLocalIP(), TestFarmUnknownIPDroppedOrStatus() (+1 more)

### Community 116 - "auto_setup: done.go"
Cohesion: 0.36
Nodes (6): judgeDoneRecord(), markDone(), parseDoneRecord(), validHostName(), writeDoneRecord(), doneRecord

### Community 117 - "auto_setup: loadNewhostHistory()"
Cohesion: 0.31
Nodes (8): loadNewhostHistory(), newhostHistFiles(), nhAlnum(), nhPrefix(), TestNewhostRecommend(), writeHist(), nhHist, nhHistIndex

### Community 118 - "auto_setup: StageCounts"
Cohesion: 0.36
Nodes (3): StageCounts, filterSnapshotUser(), userFilterSource

### Community 119 - "awx_script: fake_dns.py"
Cohesion: 0.39
Nodes (4): enc_name(), load(), main(), read_name()

### Community 120 - "config_check: run_auto_test.sh"
Cohesion: 0.33
Nodes (8): AUTO_SETUP_DIR, bad(), chk(), GOSSH_LOG, make_copy(), ok(), PATH, run_auto_test.sh script

### Community 121 - "ldap_check: ldap_check.sh"
Cohesion: 0.39
Nodes (7): conf_get(), conf_list(), ini_get(), report(), s4_has(), same_set(), ldap_check.sh script

### Community 122 - "ldap_setting: lib_common.sh"
Cohesion: 0.36
Nodes (7): changed_has(), fail(), log(), mark_changed(), restart_for_changed(), restart_svc(), lib_common.sh script

### Community 123 - "auto_setup: nopNotifier"
Cohesion: 0.36
Nodes (5): nopNotifier, envValue(), wallLocale(), TestWallLocale(), wallNotifier

### Community 124 - "auto_setup: extractSecondTar()"
Cohesion: 0.46
Nodes (7): extractSecondTar(), TestExtractSecondTarBroken(), liveSetup(), liveWrite(), TestOS6ViaLoginShell(), TestOS6WrappersViaLoginShell(), TestRemoteRunViaLoginShell()

### Community 125 - "auto_setup: run_setup_scripts.sh"
Cohesion: 0.43
Nodes (6): chk(), newproj(), ng(), NO_COLOR, ok(), run_setup_scripts.sh script

### Community 126 - "ldap_setting: test_all.sh"
Cohesion: 0.46
Nodes (7): activate(), apply(), check(), make_fixture(), ng(), ok(), test_all.sh script

### Community 127 - "auto_setup: argAfter()"
Cohesion: 0.67
Nodes (3): argAfter(), chgCall, chgWorld

### Community 128 - "auto_setup: newDefaultDaemon()"
Cohesion: 0.29
Nodes (7): newDefaultDaemon(), runDaemon(), writePid(), runEnsure(), tryLock(), withStdPath(), lockPath()

### Community 129 - "auto_setup: realWorkScript()"
Cohesion: 0.33
Nodes (7): realWorkScript(), stripScriptLog(), TestStripScriptLog(), TestWorkScriptReal(), TestWorkShellLineAndEnv(), workScriptEnv(), workShellLine()

### Community 130 - "auto_setup: make_demo_jobs.sh"
Cohesion: 0.53
Nodes (4): dh(), host(), mkjob(), make_demo_jobs.sh script

### Community 132 - "copy: send.sh"
Cohesion: 0.33
Nodes (4): send.sh script, GOPROXY, GOTOOLCHAIN, setup.sh script

### Community 133 - "ldap_check: test_check.sh"
Cohesion: 0.60
Nodes (5): check(), make_good(), ng(), ok(), test_check.sh script

### Community 135 - "ip_change: ip_change/internal/render/ap"
Cohesion: 0.80
Nodes (4): detect_os_major(), fail_out(), set_ifcfg_key(), apply_body.sh script

### Community 136 - "조사: run_survey.sh"
Cohesion: 0.60
Nodes (3): conf_get(), dbg(), run_survey.sh script

### Community 137 - "auto_setup: auto_setup/setup.sh"
Cohesion: 0.83
Nodes (3): build_engine(), install_sample(), setup.sh script

### Community 139 - "ip_change: ip_change/internal/render/ro"
Cohesion: 0.83
Nodes (3): detect_os_major(), fail_out(), rollback_body.sh script

### Community 140 - "ip_change: run_ip_change.sh"
Cohesion: 0.83
Nodes (3): select_user_context(), run_ip_change.sh script, usage()

### Community 141 - "ip_change: ip_change/setup.sh"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

### Community 142 - "ldap_setting: ldap_setting/internal/render"
Cohesion: 0.83
Nodes (3): all_stamps(), restore_one(), rollback_body.sh script

### Community 143 - "ldap_setting: deploy_ldap.sh"
Cohesion: 0.83
Nodes (3): select_user_context(), deploy_ldap.sh script, usage()

### Community 144 - "ldap_setting: scripts/run.sh"
Cohesion: 0.83
Nodes (3): select_infra(), select_user_context(), run.sh script

### Community 145 - "ldap_setting: ldap_setting/setup.sh"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

### Community 146 - "redfish: redfish/setup.sh"
Cohesion: 0.50
Nodes (3): GOFLAGS, GOPROXY, setup.sh script

### Community 147 - "패스워드변경자동화: 패스워드변경자동화/run.sh"
Cohesion: 0.83
Nodes (3): get_opt(), run.sh script, usage()

### Community 149 - "auto_setup: parseWorkOS6Over()"
Cohesion: 1.00
Nodes (3): parseWorkOS6Over(), TestParseWorkOS6Over(), workOS6Over()

## Knowledge Gaps
- **37 isolated node(s):** `bgTask[T]`, `auto_setup`, `GOPROXY`, `CGO_ENABLED`, `E2E_SCEN` (+32 more)
  These have ≤1 connection - possible missing edges. (Counts symbols only; 261 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **24 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Daemon` connect `데몬·시계·job` to `auto_setup: newDefaultDaemon()`, `AWX 실행·설정 경로`, `auto_setup: .Set()`, `auto_setup: .refreshStages()`, `auto_setup: TestParseCheckFQDNAndMismatc`, `auto_setup: Checker`, `데몬 단계 전이 테스트`, `auto_setup: anaconda_test.go`, `auto_setup: .acceptManualRun()`, `auto_setup: fakeReply`, `auto_setup: relayArgsOK()`?**
  _High betweenness centrality (0.025) - this node is a cross-community bridge._
- **What connects `bgTask[T]`, `auto_setup`, `GOPROXY` to the rest of the system?**
  _37 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `AWX 실행·설정 경로` be split into smaller, more focused modules?**
  _Cohesion score 0.08577165806081469 - nodes in this community are weakly interconnected._
- **Why does `tuiState` connect `AWX 프로파일·완료 TUI` to `AWX 실행·설정 경로`, `auto_setup: change.go`, `auto_setup: validChangeUser()`, `auto_setup: .Set()`, `auto_setup: envMap()`, `AWX 실행 테스트`, `auto_setup: .key()`, `TUI 메뉴·골든 테스트`, `auto_setup: newSource()`, `auto_setup: inlineMenu`, `auto_setup: .doneFile()`, `auto_setup: change_tui.go`, `redfish 모의 BMC 테스트`, `auto_setup: .runWorkDraft()`, `auto_setup: change_tui_test.go`, `auto_setup: .cancel()`, `auto_setup: TestChangeHintStyleAndHelp()`?**
  _High betweenness centrality (0.016) - this node is a cross-community bridge._
- **Should `AWX 프로파일·완료 TUI` be split into smaller, more focused modules?**
  _Cohesion score 0.06561859193438141 - nodes in this community are weakly interconnected._
- **Why does `newhostState` connect `auto_setup: .key()` to `AWX 실행·설정 경로`, `AWX 프로파일·완료 TUI`, `auto_setup: .Set()`, `auto_setup: nhNormMAC()`, `auto_setup: envMap()`, `auto_setup: bgTask`, `auto_setup: CheckDNS()`, `변경 화면 렌더링`, `auto_setup: newhost.go`, `auto_setup: loadNewhostHistory()`?**
  _High betweenness centrality (0.008) - this node is a cross-community bridge._
- **Should `redfish 모의 BMC·적용` be split into smaller, more focused modules?**
  _Cohesion score 0.13263157894736843 - nodes in this community are weakly interconnected._