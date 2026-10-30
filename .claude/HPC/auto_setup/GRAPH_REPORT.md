# Graph Report - auto_setup  (2026-10-30)

## Corpus Check
- cluster-only mode — file stats not available

## Summary
- 2070 nodes · 7996 edges · 80 communities (72 shown, 8 thin omitted)
- Extraction: 74% EXTRACTED · 26% INFERRED · 0% AMBIGUOUS · INFERRED: 2065 edges (avg confidence: 0.85)
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
- Community 58
- Community 59
- Community 60
- Community 61
- Community 62
- Community 63
- Community 64
- Community 65
- Community 66
- Community 67
- Community 68
- Community 69
- Community 70
- Community 71
- Community 72
- Community 73
- Community 74
- Community 75
- Community 76
- Community 77
- Community 78
- Community 79

## God Nodes (most connected - your core abstractions)
1. `tuiState` - 95 edges
2. `setupDir()` - 87 edges
3. `Daemon` - 62 edges
4. `twoGroupSnap()` - 59 edges
5. `newhostState` - 55 edges
6. `drive()` - 54 edges
7. `logf()` - 54 edges
8. `Job` - 53 edges
9. `Snapshot` - 52 edges
10. `changeState` - 52 edges

## Surprising Connections (you probably didn't know these)
- `titleLines()` --calls--> `headerUser()`  [INFERRED]
  render.go → sessionuser.go
- `TestApplyCheckAnacondaWithoutSeenDown()` --calls--> `Stage`  [INFERRED]
  anaconda_test.go → model.go
- `TestRecheckAnacondaShowsStageChange()` --calls--> `Stage`  [INFERRED]
  anaconda_test.go → model.go
- `TestLocalDownFallsBackToOS6Ping()` --calls--> `Stage`  [INFERRED]
  route_test.go → model.go
- `TestNoOS6KeepsLocalOnly()` --calls--> `Stage`  [INFERRED]
  route_test.go → model.go

## Import Cycles
- None detected.

## Communities (80 total, 8 thin omitted)

### Community 0 - "Community 0"
Cohesion: 0.05
Nodes (84): build_engine(), CGO_ENABLED, E2E_SCEN, GOPROXY, demo_change.sh script, ts(), E2E_SCEN, demo_flow.sh script (+76 more)

### Community 1 - "Community 1"
Cohesion: 0.06
Nodes (82): awxCall, errAfter, fakeOneoff, fakeSrc, reqCall, rig, wrapCase, TestAwxEscCancelsPrompt() (+74 more)

### Community 2 - "Community 2"
Cohesion: 0.07
Nodes (81): CheckResult, lbEntry, lbEnv, lbInfo, ldapMeta, ldapMetaFile, LdapState, realChecker (+73 more)

### Community 3 - "Community 3"
Cohesion: 0.06
Nodes (55): dnsAns, dnsConf, DNSEntry, DNSHostResult, dnsKind, dnsProtoError, dnsRecords, DNSReport (+47 more)

### Community 4 - "Community 4"
Cohesion: 0.06
Nodes (58): nopSecond, oneoffProc, realRunner, realSecond, Runner, RunResult, SecondResult, realOneoffStart() (+50 more)

### Community 5 - "Community 5"
Cohesion: 0.07
Nodes (55): TestChangeOS6WrapperEscape(), TestChangeRealExecCancelKillsGroup(), NewRealChecker(), fakeCheckBin(), fakeExternal(), TestCheckLocal(), TestCheckLocalExecFailure(), TestCheckOS6() (+47 more)

### Community 6 - "Community 6"
Cohesion: 0.07
Nodes (44): localSource, remoteResult, remoteSource, parseRemote(), relayArg(), relayRemote(), remoteBin(), remoteCommand() (+36 more)

### Community 7 - "Community 7"
Cohesion: 0.12
Nodes (47): _apply_changes(), ask_var(), _awk_target(), backup_file(), bad_usage(), check_targets(), check_value(), confirm() (+39 more)

### Community 8 - "Community 8"
Cohesion: 0.12
Nodes (13): Clock, Daemon, hostRT, inflight, runProgress, active(), pickIP(), logf() (+5 more)

### Community 9 - "Community 9"
Cohesion: 0.08
Nodes (24): gateIn, icmpPinger, noHostErr, os6Sessions, PingResult, PingTarget, realPinger, scriptPinger (+16 more)

### Community 10 - "Community 10"
Cohesion: 0.27
Nodes (41): TestRecheckAnacondaClosedJobKeepsStage(), drive(), install(), newTestDaemon(), newWorld(), scenario7(), submit(), TestCancelDropsJob() (+33 more)

### Community 11 - "Community 11"
Cohesion: 0.12
Nodes (41): viewState, renderAwx(), TestAwxLayoutNoEllipsis(), changeMenuLines(), TestChangeHintStyleAndHelp(), TestChangeMenuInlineGolden(), TestChangeMenuLinesWrapNoTruncation(), TestInlineMenuOpenCloseByteIdentical() (+33 more)

### Community 12 - "Community 12"
Cohesion: 0.13
Nodes (36): atom, seg, SnapHost, padTo(), RenderDNSTable(), TestHintSegsColors(), inputLines(), TestOneoffLayoutHostEchoSameAsPacking() (+28 more)

### Community 13 - "Community 13"
Cohesion: 0.12
Nodes (19): changeJob, changePlan, changeRecord, chgHost, chgRunner, recFile, secretMask, changeAutofill() (+11 more)

### Community 14 - "Community 14"
Cohesion: 0.11
Nodes (30): workPlanT, workRec, workRelayRec, workRes, workRunReq, workStore, commaN(), gunzipBytes() (+22 more)

### Community 15 - "Community 15"
Cohesion: 0.19
Nodes (31): newChangeState(), renderChange(), chgRenderStates(), TestChangeRenderHeightFits(), TestChangeRenderRedDanger(), TestChangeRenderWrapNoTruncation(), TestChangeCancelDuringRun(), TestChangeDryRunCancelBackToInput() (+23 more)

### Community 16 - "Community 16"
Cohesion: 0.14
Nodes (31): grpRef, selection, SnapGroup, SnapJob, StageCounts, TestRenderDuplicateDaemonWarning(), barSegs(), clockOf() (+23 more)

### Community 17 - "Community 17"
Cohesion: 0.11
Nodes (24): ioctl(), isTTY(), killGroup(), makeRaw(), notifyResize(), oneoffSysProcAttr(), sanitizeTermios(), getTermios() (+16 more)

### Community 18 - "Community 18"
Cohesion: 0.23
Nodes (33): Snapshot, TestChangeHintWrapsNoTruncation(), TestMenuHintAndMenuLinesWrap(), flatGroups(), allRenders(), renderAll(), sampleAllDone(), sampleKorean() (+25 more)

### Community 20 - "Community 20"
Cohesion: 0.10
Nodes (25): chg_fx_host(), chg_make_nodes(), chg_write_conf(), lib_change.sh script, build(), CGO_ENABLED, GOPROXY, ng() (+17 more)

### Community 21 - "Community 21"
Cohesion: 0.10
Nodes (26): userFilterSource, setProfiles(), TestConfVarsHasAwxProfiles(), TestParseAwxProfiles(), TestParseAwxProfilesNine(), lastLines(), menuTUI(), TestAwxMenuIgnoresOtherKeys() (+18 more)

### Community 23 - "Community 23"
Cohesion: 0.14
Nodes (4): doneState, tuiState, newhostDirProblem(), hasManual()

### Community 24 - "Community 24"
Cohesion: 0.22
Nodes (4): flowScroll, newhostState, rowNums(), TestNewhostGolden()

### Community 25 - "Community 25"
Cohesion: 0.17
Nodes (6): workDraft, workNote, workState, newWorkState(), goldenWorkState(), workNotes()

### Community 26 - "Community 26"
Cohesion: 0.16
Nodes (27): changeMenuItems(), TestChangeMenuItemsVisibility(), changeEnabled(), changeConfDir(), changeConfPath(), confPaths(), confTrue(), confVars() (+19 more)

### Community 27 - "Community 27"
Cohesion: 0.13
Nodes (28): doneAppliedDir(), doneRecDir(), logCount(), putDone(), TestDoneRecordAcceptedExcludedFromRun(), TestDoneRecordFutureRejected(), TestDoneRecordRejected(), TestMarkDoneManual() (+20 more)

### Community 28 - "Community 28"
Cohesion: 0.11
Nodes (23): changeEnv, changeExecFn, changeKind, changeMenuItem, ipEntry, rbOption, assetSites(), changeTracked() (+15 more)

### Community 29 - "Community 29"
Cohesion: 0.17
Nodes (25): fakeLdap, fakeSecond, xworld, hostStage(), newX(), newXDaemon(), secondScenario(), TestDaemonStuckThenReady() (+17 more)

### Community 30 - "Community 30"
Cohesion: 0.12
Nodes (24): nhHist, nhHistIndex, nhOp, nhSaveResult, nhVals, loadNewhostHistory(), newhostHistFiles(), newhostSave() (+16 more)

### Community 31 - "Community 31"
Cohesion: 0.20
Nodes (21): buildFailure, cliOut, buildBins(), prepState(), runBin(), stubEnv(), TestMain(), TestRemoteDoneAndRequest() (+13 more)

### Community 32 - "Community 32"
Cohesion: 0.15
Nodes (21): NewhostTpl, nhTpl, addNewhostTemplate(), loadNewhostTemplates(), nhComplete(), nhParsePlus(), nhSetCol(), nhTplConflict() (+13 more)

### Community 33 - "Community 33"
Cohesion: 0.17
Nodes (17): GroupView, Host, Recheck, RecheckHost, SnapDaemon, SnapManual, Stage, secs() (+9 more)

### Community 34 - "Community 34"
Cohesion: 0.30
Nodes (17): nhH, envMap(), nhFakeDNS(), nhSetup(), readLog(), TestNewhostAssignLanguage(), TestNewhostAwxProfiles(), TestNewhostBigPaste() (+9 more)

### Community 35 - "Community 35"
Cohesion: 0.13
Nodes (10): Checker, Job, LdapBackup, nopLdap, Notifier, Pinger, realClock, cloneJob() (+2 more)

### Community 36 - "Community 36"
Cohesion: 0.23
Nodes (16): chgCall, chgRig, chgWorld, ipEngineArgs(), ldapEngineArgs(), argAfter(), chgRes(), chgRun() (+8 more)

### Community 37 - "Community 37"
Cohesion: 0.23
Nodes (20): TestPidFileAndStopDaemon(), isRunning(), newDefaultDaemon(), pidPath(), pidPathIn(), processAlive(), readPid(), readPidIn() (+12 more)

### Community 38 - "Community 38"
Cohesion: 0.18
Nodes (10): blockReader, buildEchoRequest(), icmpChecksum(), NewRealPinger(), parseEchoReply(), TestBuildParseEcho(), TestIcmpChecksumRFC1071(), TestParseEchoReplyIPHeaderAndCorrupt() (+2 more)

### Community 39 - "Community 39"
Cohesion: 0.17
Nodes (12): Group, jobSpec, Second, snapFixture(), TestParseJobSpecGroups(), TestSnapshotDeterministic(), formatJobStatus(), hostState() (+4 more)

### Community 40 - "Community 40"
Cohesion: 0.20
Nodes (18): TestWorkCleanOut(), TestWorkCleanTmp(), TestWorkCleanTmpLegacy(), TestWorkCLI(), TestWorkExecuteRoutes(), TestWorkNewNameAvoidsCollisions(), TestWorkOS6CommandQuoting(), TestWorkParseHosts() (+10 more)

### Community 41 - "Community 41"
Cohesion: 0.16
Nodes (6): fakeClock, fakeConn, fakeReply, newFakeConn(), workCleanAll(), workCleanOut()

### Community 43 - "Community 43"
Cohesion: 0.24
Nodes (11): remoteWorkStore, TestRemoteCommandQuoting(), cmdWorkRelay(), relayInProcess(), TestWkEncNoDangerWords(), TestWorkDanger(), TestWorkRelayRoundTrip(), wkDec() (+3 more)

### Community 44 - "Community 44"
Cohesion: 0.15
Nodes (8): SnapshotSource, tuiEnv, newSource(), cmdNewhost(), TestNewhostCLI(), realTUIEnv(), runTUILoopWith(), cmdWork()

### Community 45 - "Community 45"
Cohesion: 0.12
Nodes (16): run, jobs, test, name, on, pull_request, push, working-directory (+8 more)

### Community 46 - "Community 46"
Cohesion: 0.21
Nodes (9): AwxProfile, awxState, nhAwxReq, oneoffState, parseAwxProfiles(), awxEnv(), newhostAwxEnv(), newhostAwxNote() (+1 more)

### Community 47 - "Community 47"
Cohesion: 0.33
Nodes (15): changeCmd, changeOut, engHost, emptyGossh(), engineErr(), failMap(), hostSet(), joinDetail() (+7 more)

### Community 48 - "Community 48"
Cohesion: 0.17
Nodes (10): countLine(), rsLabel(), menuLines(), TestMenuInlineHeight(), TestMenusGolden(), TestOneoffLayoutWideSameAsBefore(), rMin(), goldenText() (+2 more)

### Community 49 - "Community 49"
Cohesion: 0.28
Nodes (10): doneSnap(), doneTUI(), chgTUI, TestDoneHintAndPromptWidths(), TestDoneKeyAllViewAndNoTargets(), TestDoneKeyCancelAndIgnore(), TestDoneKeyExclusiveWithMenuAndOthers(), TestDoneKeyHostAndGroup() (+2 more)

### Community 50 - "Community 50"
Cohesion: 0.18
Nodes (11): workDB, workGroup, shortHost(), TestWorkSameListReuse(), workPrepareTmp(), workListDir(), workMatchHost(), workNewName() (+3 more)

### Community 51 - "Community 51"
Cohesion: 0.23
Nodes (5): nhFindTpl(), nhLine(), nhTplLabel(), nhPad(), renderNewhost()

### Community 52 - "Community 52"
Cohesion: 0.41
Nodes (13): newSessionTUI(), saveSessionState(), TestAskUserCLI(), TestHeaderUser(), TestNeedSessionUserAsksOnceAndRemembers(), TestNeedSessionUserCancelAndInvalid(), TestNeedSessionUserDirectNameWithoutMenu(), TestNeedSessionUserForceUser() (+5 more)

### Community 53 - "Community 53"
Cohesion: 0.30
Nodes (14): jobIDAt(), oldOS(), TestClaimedRejectAndAccept(), TestRequestAlreadyClaimedIsSkipped(), TestManualCheckOnlyKeepsHostsUndone(), claimRequest(), reqClaimedDir(), reqRejectedDir() (+6 more)

### Community 54 - "Community 54"
Cohesion: 0.24
Nodes (10): groupHosts(), listReqFiles(), parseRequest(), readActiveRequests(), recheckDir(), rejectRequest(), reqActiveDir(), unclaimRequest() (+2 more)

### Community 55 - "Community 55"
Cohesion: 0.37
Nodes (11): s6Screens, s6T, s6Copy(), s6DirsEqual(), s6Glob(), s6Has(), s6Read(), s6Run() (+3 more)

### Community 56 - "Community 56"
Cohesion: 0.26
Nodes (10): gosshLine, gosshOut, clipWhy(), logGosshUnparsed(), parseCheck(), parseGossh(), TestParseCheck(), TestParseCheckWhy() (+2 more)

### Community 57 - "Community 57"
Cohesion: 0.26
Nodes (6): runCall, world, simHost, TestBootWaitAndLateBatch(), TestRestartRestore(), wantRuns()

### Community 58 - "Community 58"
Cohesion: 0.31
Nodes (3): localWorkStore, mkdir0700(), workWriteFile()

### Community 59 - "Community 59"
Cohesion: 0.24
Nodes (11): nhRow, nhCheckDups(), nhNormMAC(), nhParseLine(), nhTargets(), nhRowsOf(), TestNhCheckDups(), TestNhParseLine() (+3 more)

### Community 60 - "Community 60"
Cohesion: 0.33
Nodes (5): Run, findSnapJob(), lastManualRun(), recheckLines(), recheckTarget()

### Community 61 - "Community 61"
Cohesion: 0.24
Nodes (5): bgTask, flowFrame, pasteBuf, pasteEv, startBG()

### Community 62 - "Community 62"
Cohesion: 0.33
Nodes (6): inlineMenu, menuItem, awxMenu(), hostKey(), hostMenu(), lowerKey()

### Community 64 - "Community 64"
Cohesion: 0.40
Nodes (7): wkTUI, fakeGosshRun(), gosshOutput(), newWorkTUI(), TestWorkDangerAndBigAsk(), TestWorkNewRunReuseDeriveMemoSearch(), TestWorkOpenWithForceUserAndSessionUser()

### Community 65 - "Community 65"
Cohesion: 0.27
Nodes (9): validChangeUser(), parseUserRoute(), pickUser(), TestParseUserRoute(), userMenu(), askUserCLI(), fixedUser(), headerUser() (+1 more)

### Community 66 - "Community 66"
Cohesion: 0.36
Nodes (5): runDone, selectSecondTargets(), unreachableLine(), wallMessage(), uniq()

### Community 67 - "Community 67"
Cohesion: 0.46
Nodes (7): doneRecord, judgeDoneRecord(), markDone(), parseDoneRecord(), TestJudgeDoneRecord(), validHostName(), writeDoneRecord()

### Community 68 - "Community 68"
Cohesion: 0.43
Nodes (5): fdCase, tlSeg, firstDownCases(), simHost, TestFirstDownScenarios()

### Community 69 - "Community 69"
Cohesion: 0.36
Nodes (5): nopNotifier, wallNotifier, envValue(), wallLocale(), TestWallLocale()

### Community 70 - "Community 70"
Cohesion: 0.43
Nodes (6): chk(), newproj(), ng(), NO_COLOR, ok(), run_setup_scripts.sh script

### Community 71 - "Community 71"
Cohesion: 0.29
Nodes (8): realWorkScript(), stripScriptLog(), TestStripScriptLog(), TestWorkScriptReal(), TestWorkShellLineAndEnv(), workRunOnce(), workScriptEnv(), workShellLine()

### Community 72 - "Community 72"
Cohesion: 0.53
Nodes (4): queuedDaemon(), TestApplyCheckAnacondaWithoutSeenDown(), TestApplyCheckNormalResponseKeepsQueued(), TestRecheckAnacondaShowsStageChange()

### Community 73 - "Community 73"
Cohesion: 0.53
Nodes (4): dh(), host(), mkjob(), make_demo_jobs.sh script

### Community 75 - "Community 75"
Cohesion: 0.83
Nodes (3): setPathVars(), TestDhcpCandidates(), TestOS6OSCheckPath()

### Community 76 - "Community 76"
Cohesion: 0.83
Nodes (3): build_engine(), install_sample(), setup.sh script

## Knowledge Gaps
- **17 isolated node(s):** `CGO_ENABLED`, `E2E_SCEN`, `GOPROXY`, `E2E_SCEN`, `AUTO_SETUP_DIR` (+12 more)
  These have ≤1 connection - possible missing edges. (Counts symbols only; 110 node(s) total have ≤1 connection when file, concept and rationale nodes are included.)
- **8 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `tuiState` connect `Community 23` to `Community 1`, `Community 9`, `Community 11`, `Community 16`, `Community 18`, `Community 19`, `Community 22`, `Community 24`, `Community 25`, `Community 28`, `Community 34`, `Community 41`, `Community 42`, `Community 44`, `Community 46`, `Community 48`, `Community 49`, `Community 52`, `Community 60`, `Community 62`, `Community 63`, `Community 64`?**
  _High betweenness centrality (0.047) - this node is a cross-community bridge._
- **Are the 81 inferred relationships involving `setupDir()` (e.g. with `queuedDaemon()` and `TestRecheckAnacondaClosedJobKeepsStage()`) actually correct?**
  _`setupDir()` has 81 INFERRED edges - model-reasoned connections that need verification._
- **What connects `CGO_ENABLED`, `E2E_SCEN`, `GOPROXY` to the rest of the system?**
  _17 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `Community 0` be split into smaller, more focused modules?**
  _Cohesion score 0.05115758028379388 - nodes in this community are weakly interconnected._
- **Why does `changeState` connect `Community 22` to `Community 36`, `Community 41`, `Community 13`, `Community 15`, `Community 48`, `Community 55`, `Community 23`, `Community 28`?**
  _High betweenness centrality (0.032) - this node is a cross-community bridge._
- **Should `Community 1` be split into smaller, more focused modules?**
  _Cohesion score 0.05558728345707215 - nodes in this community are weakly interconnected._
- **Why does `logf()` connect `Community 8` to `Community 66`, `Community 35`, `Community 2`, `Community 4`, `Community 69`, `Community 6`, `Community 37`, `Community 9`, `Community 67`, `Community 43`, `Community 41`, `Community 14`, `Community 19`, `Community 53`, `Community 54`, `Community 24`, `Community 56`, `Community 27`?**
  _High betweenness centrality (0.022) - this node is a cross-community bridge._