# Test Java chưa port sang Go

Danh sách này liệt kê mọi test Java của `aisdlc-cli` và `aisdlc-core` chưa có bản Go sau giai đoạn 1–2b, kèm giai đoạn port và lý do. Giai đoạn 3 và giai đoạn 4 đã port xong; mỗi mục ghi tên test Go tương ứng.

Cách đối chiếu (2026-11-06, nhánh `go-phase2b`):
- Mỗi phương thức `@Test` Java được so với tên `TestXxx` của Go: tên Java viết PascalCase, bỏ `_`, thêm `Test`.
- Test không khớp tên được tra tay, vì nhiều test giai đoạn 1 port với tiền tố lớp. Ví dụ `DesignValidatorTest.noSequence` thành `TestDesignNoSequence`, `GlobOverlapTest` thành bảng trong `TestGlobOverlap`.
- Những test đã port dưới tên khác không nằm trong danh sách này.

## Không port vì thiết kế Go khác

| Test Java | Lý do |
|---|---|
| `GateServiceTest.suiteLookupSkipsExcludedDirsAndIsSorted` | Bản Go không đưa file suite case vào artifact của cổng CASES. `TestCasesDoNotDependOnSuiteFiles` kiểm điều này. |

## Port một phần, phần còn lại port ở giai đoạn 4

Các test dưới đây port làm hai lần. Phần cần web đã port ở giai đoạn 4 (2026-11-06, nhánh `go-phase4b`).

- `RiskCommandTest.lowerWithReason_writesOverrideAndEvent`: phần override ở `TestLowerWithReasonWritesOverrideAndEvent`, phần event qua web giả ở `TestLowerWithReasonSendsRiskEvents`.
- `HookCommandTest`: phần event của `sessionStartStillOutputsContextAndEmitsEvent`, `stopBlockedEmitsNoSessionStopButAllowedDoes` và `postToolUseOutsideAisdlcRepoSilent` đã bổ sung vào `TestSessionStartStillOutputsContextAndEmitsEvent`, `TestStopBlockedEmitsNoSessionStopButAllowedDoes` và `TestPostToolUseOutsideAisdlcRepoSilent`.
- `WebGatesTest`: bốn test chỉ đọc cache ở `internal/cli/webcache_test.go`. Phần hỏi web của `validModeCacheBeatsConfigLocal` ở `TestValidModeCacheBeatsConfigLocalAsksWebOnce`; phần refresh của `gateEnvelopeSignedByOtherKeyDoesNotSwitchMode` ở `TestGateEnvelopeSignedByOtherKeyIsRefusedByRefresh`.
- `EvalCommandTest`: sáu test cổng REVIEW đã port, thay `aisdlc eval` bằng báo cáo eval viết tay. Xem `internal/cli/review_gate_test.go`.

## Giai đoạn 3 (harness Go, `aisdlc eval`): 65 test, đã port

Giai đoạn 3 xong (2026-11-06, nhánh `go-phase3`). Mỗi test Java dưới đây được ghép với test Go tương ứng; test Go nằm ở `internal/eval/*_test.go`, `internal/cli/eval_cmd_test.go`, `internal/cli/runner_test.go`, `internal/harness/*_test.go` và `internal/lang/golang/*_test.go`. Hai test Surefire không có tương đương, lý do ghi ở bảng.

45 test của `aisdlc-junit` (bộ chạy case JUnit) đã port vào `internal/harness` (`executor_test.go`, `checks_test.go`, `pgdb_test.go`, `kafka_test.go`, `jsoncmp_test.go`, `results_test.go`, `drafts_test.go`, `schema_test.go`, `harness_test.go`). Không lập bảng từng test.

Dịch vụ mẫu e2e (`e2e/fixture-service`, Go) có `TestFixtureServiceFeeCases` trong `internal/harness/fixture_test.go`.

Các test cần `web != nil` (duyệt qua web) không nằm trong 65 test này; chúng ở mục giai đoạn 4 bên dưới.

### EvalCommandTest (aisdlc-cli, 10)

| Test Java | Test Go |
|---|---|
| `evalWritesReportAndExitsByResult` | `TestEvalWritesReportAndExitsByResult` |
| `ciEvaluatesChangedFeaturesWithoutActiveFeature` | `TestEvalCiEvaluatesChangedFeaturesWithoutActiveFeature` |
| `ciWithoutChangedFeaturesDoesNothing` | `TestEvalCiWithoutChangedFeaturesDoesNothing` |
| `localEvalNeedsActiveFeature` | `TestEvalLocalNeedsActiveFeature` |
| `ciCodeChangeWithoutFeatureFails` | `TestEvalCiCodeChangeWithoutFeatureFails` |
| `ciCodeChangeWithoutFeatureAllowedWhenDisabled` | `TestEvalCiCodeChangeWithoutFeatureAllowedWhenDisabled` |
| `ciConfigChangeDisablingFeatureRequirementWithCodeFails` | `TestEvalCiConfigChangeDisablingFeatureRequirementWithCodeFails` |
| `ciConfigChangeMakingEverythingUnmanagedWithCodeFails` | `TestEvalCiConfigChangeMakingEverythingUnmanagedWithCodeFails` |
| `ciUsesBaseConfigNotChangedOneEvenWithoutCodeChange` | `TestEvalCiUsesBaseConfigNotChangedOneEvenWithoutCodeChange` |
| `ciReportWarnsAboutUnapprovedOrChangedGates` | `TestEvalCiReportWarnsAboutUnapprovedOrChangedGates` |

### CiAutoApprovalTest (aisdlc-core, 5)

| Test Java | Test Go |
|---|---|
| `ci_autoApprovalMatchingFlow_pass` | `TestCiAutoApprovalMatchingFlowPass` |
| `ci_autoApprovalNotMatching_fail` | `TestCiAutoApprovalNotMatchingFail` |
| `ci_autoApprovalWithoutRiskYaml_fail` | `TestCiAutoApprovalWithoutRiskYamlFail` |
| `ci_manualApprovals_noAutoCheck` | `TestCiManualApprovalsNoAutoCheck` |
| `localEval_doesNotCheckAutoApprovals` | `TestLocalEvalDoesNotCheckAutoApprovals` |

### EvalServiceTest (aisdlc-core, 35)

| Test Java | Test Go |
|---|---|
| `allGreenPasses` | `TestEvalAllGreenPasses` |
| `failingCaseFailsCasesAllPass` | `TestEvalFailingCaseFailsCasesAllPass` |
| `caseMissingFromResultsFailsCasesAllPass` | `TestEvalCaseMissingFromResultsFailsCasesAllPass` |
| `emptyResultsFailCasesAllPass` | `TestEvalEmptyResultsFailCasesAllPass` |
| `editedCasesFailsLockLocallyButWarnsInCi` | `TestEvalEditedCasesFailsLockLocallyButWarnsInCi` |
| `unchangedCasesInCiIsOkSoftCheck` | `TestEvalUnchangedCasesInCiIsOkSoftCheck` |
| `oldTestFailureFailsBuild` | `TestEvalOldTestFailureFailsBuild` |
| `onlySuiteFailureDoesNotFailBuild` | `TestEvalOnlySuiteFailureDoesNotFailBuild` |
| `nonZeroExitWithoutTestFailuresFailsBuild` | `TestEvalNonZeroExitWithoutTestFailuresFailsBuild` |
| `extraColumnFailsSchemaMatch` | `TestEvalExtraColumnFailsSchemaMatch` |
| `missingSchemaDumpFailsSchemaMatch` | `TestEvalMissingSchemaDumpFailsSchemaMatch` |
| `bashWrittenOutOfScopeFileFailsScope` | `TestEvalBashWrittenOutOfScopeFileFailsScope` |
| `scopeExtraAllowsFile` | `TestEvalScopeExtraAllowsFile` |
| `ciScopeUsesMergeBaseOfBaseRef` | `TestEvalCiScopeUsesMergeBaseOfBaseRef` |
| `checkCommandNonZeroFails` | `TestEvalCheckCommandNonZeroFails` |
| `coverageOfChangedLinesFromJacoco` | `TestEvalCoverageOfChangedLines` |
| `coverageHardWhenRiskRequires` | `TestEvalCoverageHardWhenRiskRequires` |
| `riskFileUnreadable_hardFail` | `TestEvalRiskFileUnreadableHardFail` |
| `fullWithImpactButNoRiskFile_hardFail` | `TestEvalFullWithImpactButNoRiskFileHardFail` |
| `fullLegacyWithoutImpact_noRiskFileCheck` | `TestEvalFullLegacyWithoutImpactNoRiskFileCheck` |
| `riskCoverage_noJacoco_failsWithDistinctMessage` | `TestEvalRiskCoverageNoCoverageFailsWithDistinctMessage` |
| `riskCoverage_noNewLines_passes` | `TestEvalRiskCoverageNoNewLinesPasses` |
| `coverageHardPassesAtOrAboveMin` | `TestEvalCoverageHardPassesAtOrAboveMin` |
| `riskCoverage_fileInModuleWithoutReport_countsAsUncovered` | `TestEvalRiskCoverageFileInPackageWithoutCoverageCountsAsUncovered` |
| `riskCoverage_onlyFilesInUnmeasuredModule_fails` | `TestEvalRiskCoverageOnlyFilesInUnmeasuredPackageFails` |
| `riskCoverage_codeLessFilesInMeasuredModule_ignored` | `TestEvalRiskCoverageCodeLessFilesInMeasuredPackageIgnored` |
| `softCoverage_ignoresUnmeasuredFiles` | `TestEvalSoftCoverageIgnoresUnmeasuredFiles` |
| `coverageSoftWithoutRisk` | `TestEvalCoverageSoftWithoutRisk` |
| `reportRiskSectionResolvesLaterRequirements` | `TestEvalReportRiskSectionResolvesLaterRequirements` |
| `reportRiskSectionOkWhenCasesPassAndCoverageMet` | `TestEvalReportRiskSectionOkWhenCasesPassAndCoverageMet` |
| `configChangedInThisChangeFailsScope` | `TestEvalConfigChangedInThisChangeFailsScope` |
| `suiteFailureWhileEveryCasePassedFailsBuild` | `TestEvalSuiteFailureWhileEveryCasePassedFailsBuild` |
| `exitZeroStillReadsFreshSurefireReports` | `TestEvalExitZeroStillReadsUnitFailures` |
| `unapprovedCasesIsHardFailEvenInCi` | `TestEvalUnapprovedCasesIsHardFailEvenInCi` |
| `evaluatedTreeIgnoresFeatureDirButTracksCode` | `TestEvalEvaluatedTreeIgnoresFeatureDirButTracksCode` |

### JacocoCoverageTest (aisdlc-core, 5)

| Test Java | Test Go |
|---|---|
| `parsesLinesPerSourceFile` | `TestChangedCoverageParsesLinesPerFile` |
| `mapsChangedLinesToCoverage` | `TestChangedCoverageMapsChangedLines` |
| `onlyMainJavaFilesCount` | `TestChangedCoverageOnlySourceFilesCount` |
| `strict_filesInUnreportedModuleCountAndAreNamedUpToFive` | `TestChangedCoverageStrictUnmeasuredFilesNamedUpToFive` |
| `noMeasurableLinesIsNa` | `TestChangedCoverageNoMeasurableIsNA` |

### MavenRunnerTest (aisdlc-core, 7)

| Test Java | Test Go |
|---|---|
| `runCasesPassesContractAndReadsResults` | `TestRunDemo1Cases`, `TestResultsRoundTripAndFormat` |
| `missingResultsMakesEverySelectedCaseErrorEvenIfStaleFileExisted` | `TestReadResultsMissingFileIsNoResults`, `TestRunnerClearsStaleResults` |
| `verifyRunsVerifyWithoutTestFilter` | `TestVerifyAllGreen` |
| `selectedCaseWithoutResultRowBecomesError` | `TestReadResultsMissingRowIsError` |
| `emptyResultsMakeEverySelectedCaseError` | `TestReadResultsEmptyMakesEveryCaseError` |
| `timeoutKillsMavenAndMarksCasesError` | `TestRunTimeoutKillsServiceAndContainers`, `TestRunnerTimeoutMarksCasesError` |
| `verifyClearsStaleResultsAndReadsThemLikeRunCases` | `TestRunnerClearsStaleResults`, `TestVerifyAllGreen` |

### SurefireReportsTest (aisdlc-core, 3)

| Test Java | Test Go |
|---|---|
| `collectsFailuresAndErrorsFromEveryModule` | `TestUnitTestsCollectsFailures` |
| `staleReportsFromAnEarlierBuildAreIgnored` | không có tương đương: `go test -json` chạy mới mỗi lần nên không có báo cáo cũ để bỏ qua. |
| `casesSuiteClassMatchesSimpleOrQualifiedName` | không có tương đương: bản Go không có lớp suite case chạy qua JUnit; harness tự chạy case nên không cần khớp tên lớp. |

## Giai đoạn 4 (web client có mạng): 139 test, đã port

Giai đoạn 4 xong (2026-11-06, nhánh `go-phase4b`). Mỗi test Java dưới đây được ghép với test Go tương ứng. Test Go nằm ở `internal/cli/*_test.go`, `internal/webclient/*_test.go`, `internal/snapshot/*_test.go` và `internal/eval/*_test.go`.

Ngoài các test đơn vị (web giả `internal/webclient/fakeweb`), `TestCliAgainstGoServer` (`internal/cli/webe2e_test.go`, cần Docker) chạy CLI trên server web Go thật: `web login`, nộp BRD, lead duyệt trên web, CLI thấy approval, `web push` lên bảng, web tạm dừng chặn hook `pre-tool-use`, `eval --ci` lấy approval bằng token CI.

Chạy cả bộ: `make check` (hay `make test`) chạy `go test -p 4 ./...` (đổi bằng `TEST_P=n`): mặc định của Go mở mọi gói Docker cùng lúc và test Docker chập chờn ("wait for reaper …: context deadline exceeded"). `harnesstest.RequireDocker` nới thời hạn Ryuk (`TESTCONTAINERS_RYUK_CONNECTION_TIMEOUT=5m`, `TESTCONTAINERS_RYUK_RECONNECTION_TIMEOUT=2m`) nếu chưa đặt. `TestHookLatencyP95` (`cmd/aisdlc`) đo phần web thêm vào hook: xen kẽ hook web tắt với từng biến thể (web chết, deny, post-tool-use, backoff) và đòi p95(biến thể) − p95(web tắt) < 10 ms. Mỗi tiến trình aisdlc trả khoảng 25 ms khởi tạo testcontainers-go (đã chấp nhận), nên ngưỡng tuyệt đối p95 < 50 ms chỉ kiểm khi đặt `AISDLC_LATENCY_STRICT=1` (máy rảnh).

### CliControlTest (aisdlc-cli, 4)

| Test Java | Test Go |
|---|---|
| `webFlushLiftsPauseInLocalMode` | `TestWebFlushLiftsPauseInLocalMode` |
| `statusAppliesPendingActionsFromWeb` | `TestStatusAppliesPendingActionsFromWeb` |
| `terminalAcceptAfterWebAcceptSucceeds` | `TestTerminalAcceptAfterWebAcceptSucceeds` |
| `refreshRejectedByWebIsNotReportedAsOffline` | `TestRefreshRejectedByWebIsNotReportedAsOffline` |

### CliFlowTest (aisdlc-cli, 1)

| Test Java | Test Go |
|---|---|
| `webStatusShowsStateAndPending` | `TestWebStatusShowsStateAndPending` |

### ControlLedgerTest (aisdlc-cli, 4)

| Test Java | Test Go |
|---|---|
| `claimOnce` | `TestClaimOnce` |
| `budgetedLockGivesUpWhenAnotherProcessHoldsIt` | `TestBudgetedLockGivesUpWhenAnotherProcessHoldsIt` |
| `budgetedLockGivesUpWhenThisJvmHoldsIt` | `TestBudgetedLockGivesUpWhenThisProcessHoldsIt` |
| `resultsBecomeDone` | `TestResultsBecomeDone` |

### EvalCommandTest (aisdlc-cli, 4)

| Test Java | Test Go |
|---|---|
| `ciWebModeWithoutEnvFailsNamingVariables` | `TestEvalCiWebModeWithoutEnvFailsNamingVariables` |
| `ciConfigWebButServerPinnedLocalUsesLocalGates` | `TestEvalCiConfigWebButServerPinnedLocalUsesLocalGates` |
| `ciConfigWebServerNotPinnedFailsClosed` | `TestEvalCiConfigWebServerNotPinnedFailsClosed` |
| `ciWebModeUnreachableWebFailsAndNeverPrintsToken` | `TestEvalCiWebModeUnreachableWebFailsAndNeverPrintsToken` |

### FlowLocalTest (aisdlc-cli, 8)

| Test Java | Test Go |
|---|---|
| `webMode_cliDoesNotAutoApprove` | `TestWebModeCliDoesNotAutoApprove` |
| `webMode_submitSendsRiskLevel_andShowsServerAutoApproval` | `TestWebModeSubmitSendsRiskLevelAndShowsServerAutoApproval` |
| `webMode_submitPrintsServerAutoApproval_insteadOfAskingDev` | `TestWebModeSubmitPrintsServerAutoApprovalInsteadOfAskingDev` |
| `webMode_submitDesignRaise_submitsBeforeRiskEvent` | `TestWebModeSubmitDesignRaiseSubmitsBeforeRiskEvent` |
| `webMode_resubmitBrdAfterRaise_sendsRiskLevel` | `TestWebModeResubmitBrdAfterRaiseSendsRiskLevel` |
| `webMode_casesRaiseThenMavenThrows_stillSendsRiskEvent` | `TestWebModeCasesRaiseThenHarnessFailsStillSendsRiskEvent` |
| `webMode_statusShowsSupersededGate` | `TestWebModeStatusShowsSupersededGate` |
| `webMode_failedSubmitStillReportsRiskLevel` | `TestWebModeFailedSubmitStillReportsRiskLevel` |

### HookCommandTest (aisdlc-cli, 5)

| Test Java | Test Go |
|---|---|
| `postToolUseEmitsFileEditWithScope` | `TestPostToolUseEmitsFileEditWithScope` |
| `postToolUseBashOnlyWhenStepRunning` | `TestPostToolUseBashOnlyWhenStepRunning` |
| `postToolUseFastWhenWebDown` | `TestPostToolUseFastWhenWebDown` |
| `postToolUseSpawnsBackgroundFlushUnlessBackoff` | `TestPostToolUseSpawnsBackgroundFlushUnlessBackoff` |
| `postToolUseWithoutActiveFeatureEmitsUntaggedEditButNoBash` | `TestPostToolUseWithoutActiveFeatureEmitsUntaggedEditButNoBash` |

### HookControlTest (aisdlc-cli, 11)

| Test Java | Test Go |
|---|---|
| `pausedDenySpawnsFlushThrottled` | `TestPausedDenySpawnsFlushThrottled` |
| `instructionDeliveredOnce` | `TestInstructionDeliveredOnce` |
| `pendingAcceptAppliedByHook` | `TestPendingAcceptAppliedByHook` |
| `scopeDeniedEvent` | `TestScopeDeniedEvent` |
| `sessionIdOnEvents` | `TestSessionIdOnEvents` |
| `userPromptSubmitDeliversOnceAndSpawnsFlush` | `TestUserPromptSubmitDeliversOnceAndSpawnsFlush` |
| `userPromptSubmitSilentOutsideRepoAndWithoutControlDir` | `TestUserPromptSubmitSilentOutsideRepoAndWithoutControlDir` |
| `pausedDenyWaitsForInFlightFlushThatLiftsPause` | `TestPausedDenyWaitsForInFlightFlushThatLiftsPause` |
| `pausedDenyStillDeniedWhenInFlightFlushKeepsPause` | `TestPausedDenyStillDeniedWhenInFlightFlushKeepsPause` |
| `subagentToolCallDoesNotClaimInstructions` | `TestSubagentToolCallDoesNotClaimInstructions` |
| `heldControlLockDeniesWriteAndSkipsOtherHooks` | `TestHeldControlLockDeniesWriteAndSkipsOtherHooks` |

### HttpCiApprovalSourceTest (aisdlc-cli, 5)

| Test Java | Test Go |
|---|---|
| `parsesApprovalsAndSendsBearer` | `TestCiParsesApprovalsAndSendsBearer` |
| `parsesAutoChannelAndRule` | `TestCiParsesAutoChannelAndRule` |
| `parsesPinned` | `TestCiParsesPinned` |
| `non200IsIoExceptionWithoutToken` | `TestCiNon200IsErrorWithoutToken` |
| `unreachableIsIoException` | `TestCiUnreachableIsErrorWithoutToken` |

### OutboxTest (aisdlc-cli, 4)

| Test Java | Test Go |
|---|---|
| `capsAt2000DroppingOldestEvents` | `TestOutboxCapsAt2000DroppingOldestEvents` |
| `snapshotReplacesPrevious` | `TestOutboxSnapshotReplacesPrevious` |
| `listsSnapshotsBeforeEventsAndHonoursMax` | `TestOutboxListsSnapshotsBeforeEventsAndHonoursMax` |
| `sizeOfMissingOutboxIsZeroAndCreatesNothing` | `TestOutboxSizeOfMissingOutboxIsZeroAndCreatesNothing` |

### PolicyCommandTest (aisdlc-cli, 2)

| Test Java | Test Go |
|---|---|
| `pull_importsWhen404ThenCaches` | `TestPullImportsWhen404ThenCaches` |
| `pull_noRepoFile_importsFallbackWithoutFlow` | `TestPullNoRepoFileImportsFallbackWithoutFlow` |

### StatusControlTest (aisdlc-cli, 1)

| Test Java | Test Go |
|---|---|
| `pausedOfflineMessage` | `TestPausedOfflineMessage` |

### WebControlTest (aisdlc-cli, 5)

| Test Java | Test Go |
|---|---|
| `validEnvelopeWritesCache` | `TestValidEnvelopeWritesCache` |
| `badSignatureKeepsOldCache` | `TestControlBadSignatureKeepsOldCache` |
| `olderIssuedAtDoesNotOverwrite` | `TestOlderIssuedAtDoesNotOverwrite` |
| `inboxFlagFollowsUndeliveredInstructions` | `TestInboxFlagFollowsUndeliveredInstructions` |
| `networkErrorReturnsFalseAndStartsBackoff` | `TestControlNetworkErrorReturnsFalseAndStartsBackoff` |

### WebGatesTest (aisdlc-cli, 27)

| Test Java | Test Go |
|---|---|
| `refreshWritesVerifiedCache` | `TestRefreshWritesVerifiedCache` |
| `badSignatureKeepsOldCacheAndReturnsFalse` | `TestBadSignatureKeepsOldCacheAndReturnsFalse` |
| `envelopeOfOtherFeatureIsNotCached` | `TestEnvelopeOfOtherFeatureIsNotCached` |
| `newFeedbackAppendedOnceAcrossTwoRefreshes` | `TestNewFeedbackAppendedOnceAcrossTwoRefreshes` |
| `concurrentRefreshesAppendFeedbackOnce` | `TestConcurrentRefreshesAppendFeedbackOnce` |
| `olderEnvelopeArrivingLaterDoesNotOverwriteCache` | `TestOlderEnvelopeArrivingLaterDoesNotOverwriteCache` |
| `afterOneNetworkFailureNoMoreRequestsInProcessAndBackoffStarts` | `TestAfterOneNetworkFailureNoMoreRequestsInProcessAndBackoffStarts` |
| `migrateSendsOnlyMatchingLocalApprovalsAndSkipsReview` | `TestMigrateSendsOnlyMatchingLocalApprovalsAndSkipsReview` |
| `migrateFullFeatureSendsBrdDesignCasesSkipsReview` | `TestMigrateFullFeatureSendsBrdDesignCasesSkipsReview` |
| `submitNetworkErrorAbortsAndWritesNothing` | `TestSubmitNetworkErrorAbortsAndWritesNothing` |
| `submitSendsSnapshotThenArtifactsAndUpdatesCache` | `TestSubmitSendsSnapshotThenArtifactsAndUpdatesCache` |
| `webJsonTokenUsedForCliEvenWhenEnvTokenSetAndSubmitWarns` | `TestWebJsonTokenUsedForCliEvenWhenEnvTokenSetAndSubmitWarns` |
| `modeIssuedAtMonotonic` | `TestModeIssuedAtMonotonic` |
| `switchingConfigToWebReasksServerDespiteLocalCache` | `TestSwitchingConfigToWebReasksServerDespiteLocalCache` |
| `approveWithoutApproveTokenPrintsWebLink` | `TestApproveWithoutApproveTokenPrintsWebLink` |
| `approveConflictExitsOneWithServerMessage` | `TestApproveConflictExitsOneWithServerMessage` |
| `approveWithClaudeCodeGrantSendsClaudeCodeChannel` | `TestApproveWithClaudeCodeGrantSendsClaudeCodeChannel` |
| `approveSendsTerminalChannelAndCardFlags` | `TestApproveSendsTerminalChannelAndCardFlags` |
| `rejectInWebModeGoesToWebAndFeedbackComesBack` | `TestRejectInWebModeGoesToWebAndFeedbackComesBack` |
| `approveNotWebModeConflictExitsTwo` | `TestApproveNotWebModeConflictExitsTwo` |
| `unauthorizedAndWrongKindAreExplainedInVietnamese` | `TestUnauthorizedAndWrongKindAreExplainedInVietnamese` |
| `forbiddenWithServerReasonKeepsReason` | `TestForbiddenWithServerReasonKeepsReason` |
| `gateEnvelopeSayingLocalSwitchesCachedModeToLocal` | `TestGateEnvelopeSayingLocalSwitchesCachedModeToLocal` |
| `refreshRepinsKeyEndorsedByPinnedKeyAndRetries` | `TestRefreshRepinsKeyEndorsedByPinnedKeyAndRetries` |
| `refreshRefusesKeyNotEndorsedByPinnedKey` | `TestRefreshRefusesKeyNotEndorsedByPinnedKey` |
| `pinFirstTimeWith600` | `TestPinFirstTimeWith600` |
| `otherKeyNeedsEndorsementByPinnedKey` | `TestOtherKeyNeedsEndorsementByPinnedKey` |

### WebLoginTest (aisdlc-cli, 8)

| Test Java | Test Go |
|---|---|
| `withoutTtyExitsThree` | `TestWithoutTtyExitsThree` |
| `forbiddenTokenSaysIngestTokenNeeded` | `TestForbiddenTokenSaysIngestTokenNeeded` |
| `successWritesWebJson600AndPinsKey` | `TestSuccessWritesWebJson600AndPinsKey` |
| `rotatedKeyWithoutEndorsementIsRefused` | `TestRotatedKeyWithoutEndorsementIsRefused` |
| `approveTokenStored600` | `TestApproveTokenStored600` |
| `webJsonBeatsEnvToken` | `TestWebJsonBeatsEnvToken` |
| `envTokenOnlyStillUsedButWarned` | `TestEnvTokenOnlyStillUsedButWarned` |
| `noWarningWithoutEnvToken` | `TestNoWarningWithoutEnvToken` |

### WebPublisherTest (aisdlc-cli, 16)

| Test Java | Test Go |
|---|---|
| `batchIsVersion2AndSessionIdOnlyWhenPresent` | `TestBatchIsVersion2AndSessionIDOnlyWhenPresent` |
| `disabledWritesNothing` | `TestDisabledWritesNothing` |
| `serverDownKeepsQueueAndExitCodeUnchanged` | `TestServerDownKeepsQueueAndExitCodeUnchanged` |
| `flushSendsBacklogMax20` | `TestFlushSendsBacklogMax20` |
| `sendsBearerAndContract` | `TestSendsBearerAndContract` |
| `rejected400MovesToRejected` | `TestRejected400MovesToRejected` |
| `serverErrorKeepsFiles` | `TestServerErrorKeepsFiles` |
| `eventGoesToOutboxAndStdoutStaysClean` | `TestEventGoesToOutboxAndStdoutStaysClean` |
| `publisherErrorsGoToStderrOnly` | `TestPublisherErrorsGoToStderrOnly` |
| `eventOnlyQueuesThenSpawnsBackgroundFlush` | `TestEventOnlyQueuesThenSpawnsBackgroundFlush` |
| `eventDoesNotSpawnWhileFlushRunning` | `TestEventDoesNotSpawnWhileFlushRunning` |
| `connectionFailureStartsBackoffThatCommandsAndHooksHonour` | `TestConnectionFailureStartsBackoffThatCommandsAndHooksHonour` |
| `backoffExpires` | `TestBackoffExpires` |
| `drainSendsWholeOutboxInBatches` | `TestDrainSendsWholeOutboxInBatches` |
| `inlineFlushStopsAtOverallDeadlineOnSlowServer` | `TestInlineFlushStopsAtOverallDeadlineOnSlowServer` |
| `webFlushCommandDrainsSilently` | `TestWebFlushCommandDrainsSilently` |

### CiAutoApprovalTest (aisdlc-core, 3)

| Test Java | Test Go |
|---|---|
| `webCi_autoApprovalMatchingFlow_pass` | `TestCiAutoApprovalWebMatchingFlowPass` |
| `webCi_autoApprovalNotMatching_fail` | `TestCiAutoApprovalWebNotMatchingFail` |
| `webCi_manualApproval_noAutoCheck` | `TestCiAutoApprovalWebManualApprovalNoAutoCheck` |

### EvalServiceTest (aisdlc-core, 15)

| Test Java | Test Go |
|---|---|
| `ciWarnsOnMissingOrDriftedApprovalOfOtherGates` | `TestEvalCiWarnsOnMissingOrDriftedApprovalOfOtherGates` |
| `serverPinnedLocalWinsOverConfigWeb` | `TestEvalServerPinnedLocalWinsOverConfigWeb` |
| `serverNotPinnedAndConfigWebFailsClosed` | `TestEvalServerNotPinnedAndConfigWebFailsClosed` |
| `serverNotPinnedAndConfigLocalStaysLocal` | `TestEvalServerNotPinnedAndConfigLocalStaysLocal` |
| `webModeMissingGateIsHardFail` | `TestEvalWebModeMissingGateIsHardFail` |
| `webModeHashMismatchIsHardFail` | `TestEvalWebModeHashMismatchIsHardFail` |
| `webModeReviewTreeMismatchIsHardFail` | `TestEvalWebModeReviewTreeMismatchIsHardFail` |
| `webModeAllApprovedAndMatchingPasses` | `TestEvalWebModeAllApprovedAndMatchingPasses` |
| `webModeIgnoresLocalGatesJson` | `TestEvalWebModeIgnoresLocalGatesJson` |
| `webModeUnreachableIsHardFail` | `TestEvalWebModeUnreachableIsHardFail` |
| `webModeWithoutSourceNamesVariables` | `TestEvalWebModeWithoutSourceNamesVariables` |
| `webReportedModeWebOverridesLocalConfig` | `TestEvalWebReportedModeWebOverridesLocalConfig` |
| `sourceFailureIsHardFailEvenWhenConfigSaysLocal` | `TestEvalSourceFailureIsHardFailEvenWhenConfigSaysLocal` |
| `sourceRuntimeFailureIsHardFail` | `TestEvalSourceRuntimeFailureIsHardFail` |
| `webModeFullFlowMissingDesignIsHardFail` | `TestEvalWebModeFullFlowMissingDesignIsHardFail` |

### FeedbackLogTest (aisdlc-core, 3)

| Test Java | Test Go |
|---|---|
| `lastEntryPerGate` | `TestFeedbackLastEntryPerGate` |
| `rejectedKeepsComment` | `TestFeedbackRejectedKeepsComment` |
| `webIdMarkerIsNotPartOfComment` | `TestFeedbackWebIDMarkerIsNotPartOfComment` |

### RepoIdentityTest (aisdlc-core, 1)

| Test Java | Test Go |
|---|---|
| `localWhenNoRemoteAndOriginWhenPresent` | `TestRepoIDLocalWhenNoRemoteAndOriginWhenPresent` |

### SnapshotBuilderTest (aisdlc-core, 12)

| Test Java | Test Go |
|---|---|
| `gatesStepsCasesAndFeature` | `TestSnapshotGatesStepsCasesAndFeature` |
| `autoApprovalCarriesRule` | `TestSnapshotAutoApprovalCarriesRule` |
| `stepDiffExcludesFeatureStateAndUnmanagedFiles` | `TestSnapshotStepDiffExcludesFeatureStateAndUnmanagedFiles` |
| `stepDiffEmptyWhenOnlyStateFilesChanged` | `TestSnapshotStepDiffEmptyWhenOnlyStateFilesChanged` |
| `openQuestionsFromBrd` | `TestSnapshotOpenQuestionsFromBrd` |
| `bigArtifactIsTruncatedByUtf8Bytes` | `TestSnapshotBigArtifactIsTruncatedByUtf8Bytes` |
| `hugeDiffsAreCappedAndSnapshotFitsLimit` | `TestSnapshotHugeDiffsAreCappedAndSnapshotFitsLimit` |
| `dropsOldestDiffsWhenOverTotalLimit` | `TestSnapshotDropsOldestDiffsWhenOverTotalLimit` |
| `includesReviewCards` | `TestSnapshotIncludesReviewCards` |
| `treeOnlyWhenReviewOpen` | `TestSnapshotTreeOnlyWhenReviewOpen` |
| `noTreeInLocalModeEvenWhenReviewOpen` | `TestSnapshotNoTreeInLocalModeEvenWhenReviewOpen` |
| `oversizedReviewCardsAreDroppedLast` | `TestSnapshotOversizedReviewCardsAreDroppedLast` |
