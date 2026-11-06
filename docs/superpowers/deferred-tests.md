# Test Java chưa port sang Go

Danh sách này liệt kê mọi test Java của `aisdlc-cli` và `aisdlc-core` chưa có bản Go sau giai đoạn 1–2b (mục giai đoạn 3 đã port xong), kèm giai đoạn sẽ port và lý do.

Cách đối chiếu (2026-11-06, nhánh `go-phase2b`):
- Mỗi phương thức `@Test` Java được so với tên `TestXxx` của Go: tên Java viết PascalCase, bỏ `_`, thêm `Test`.
- Test không khớp tên được tra tay, vì nhiều test giai đoạn 1 port với tiền tố lớp. Ví dụ `DesignValidatorTest.noSequence` thành `TestDesignNoSequence`, `GlobOverlapTest` thành bảng trong `TestGlobOverlap`.
- Những test đã port dưới tên khác không nằm trong danh sách này.

## Không port vì thiết kế Go khác

| Test Java | Lý do |
|---|---|
| `GateServiceTest.suiteLookupSkipsExcludedDirsAndIsSorted` | Bản Go không đưa file suite case vào artifact của cổng CASES. `TestCasesDoNotDependOnSuiteFiles` kiểm điều này. |

## Port một phần, phần còn lại để sau

- `RiskCommandTest.lowerWithReason_writesOverrideAndEvent`: phần override đã có ở `TestLowerWithReasonWritesOverrideAndEvent`. Phần event qua FakeWeb để giai đoạn 4.
- `HookCommandTest`: phần không cần outbox đã port. Còn lại phần event của `sessionStartStillOutputsContextAndEmitsEvent`, `stopBlockedEmitsNoSessionStopButAllowedDoes` và `postToolUseOutsideAisdlcRepoSilent`, để giai đoạn 4.
- `WebGatesTest`: bốn test chỉ đọc cache đã port, xem `internal/cli/webcache_test.go`. Trong đó `validModeCacheBeatsConfigLocal` và `gateEnvelopeSignedByOtherKeyDoesNotSwitchMode` mới có phần cache; phần hỏi web và refresh để giai đoạn 4.
- `EvalCommandTest`: sáu test cổng REVIEW đã port, thay `aisdlc eval` bằng báo cáo eval viết tay. Xem `internal/cli/review_gate_test.go`.

## Giai đoạn 3 (harness Go, `aisdlc eval`): 65 test, đã port

Giai đoạn 3 xong (2026-11-06, nhánh `go-phase3`). Mỗi test Java dưới đây được ghép với test Go tương ứng; test Go nằm ở `internal/eval/*_test.go`, `internal/cli/eval_cmd_test.go`, `internal/cli/runner_test.go`, `internal/harness/*_test.go` và `internal/lang/golang/*_test.go`. Hai test Surefire không có tương đương, lý do ghi ở bảng.

45 test của `aisdlc-junit` (bộ chạy case JUnit) đã port vào `internal/harness` (`executor_test.go`, `checks_test.go`, `pgdb_test.go`, `kafka_test.go`, `jsoncmp_test.go`, `results_test.go`, `drafts_test.go`, `schema_test.go`, `harness_test.go`). Không lập bảng từng test.

Dịch vụ mẫu e2e (`e2e/fixture-service`, Go) có `TestFixtureServiceFeeCases` trong `internal/harness/fixture_test.go`.

Các test cần `web != nil` (duyệt qua web) không nằm trong 65 test này; chúng ở giai đoạn 4 bên dưới.

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

## Giai đoạn 4 (web client có mạng): 139 test

### CliControlTest (aisdlc-cli, 4)

| Test Java | Lý do |
|---|---|
| `webFlushLiftsPauseInLocalMode` | cần web flush, refresh và lệnh chờ từ web |
| `statusAppliesPendingActionsFromWeb` | cần web flush, refresh và lệnh chờ từ web |
| `terminalAcceptAfterWebAcceptSucceeds` | cần web flush, refresh và lệnh chờ từ web |
| `refreshRejectedByWebIsNotReportedAsOffline` | cần web flush, refresh và lệnh chờ từ web |

### CliFlowTest (aisdlc-cli, 1)

| Test Java | Lý do |
|---|---|
| `webStatusShowsStateAndPending` | cần `web status` và outbox |

### ControlLedgerTest (aisdlc-cli, 4)

| Test Java | Lý do |
|---|---|
| `claimOnce` | sổ điều khiển ghi được (claim, khóa) chỉ có ở giai đoạn 4 |
| `budgetedLockGivesUpWhenAnotherProcessHoldsIt` | sổ điều khiển ghi được (claim, khóa) chỉ có ở giai đoạn 4 |
| `budgetedLockGivesUpWhenThisJvmHoldsIt` | sổ điều khiển ghi được (claim, khóa) chỉ có ở giai đoạn 4 |
| `resultsBecomeDone` | sổ điều khiển ghi được (claim, khóa) chỉ có ở giai đoạn 4 |

### EvalCommandTest (aisdlc-cli, 4)

| Test Java | Lý do |
|---|---|
| `ciWebModeWithoutEnvFailsNamingVariables` | eval --ci hỏi web (chế độ, approval) qua HTTP; cần cả harness giai đoạn 3 |
| `ciConfigWebButServerPinnedLocalUsesLocalGates` | eval --ci hỏi web (chế độ, approval) qua HTTP; cần cả harness giai đoạn 3 |
| `ciConfigWebServerNotPinnedFailsClosed` | eval --ci hỏi web (chế độ, approval) qua HTTP; cần cả harness giai đoạn 3 |
| `ciWebModeUnreachableWebFailsAndNeverPrintsToken` | eval --ci hỏi web (chế độ, approval) qua HTTP; cần cả harness giai đoạn 3 |

### FlowLocalTest (aisdlc-cli, 8)

| Test Java | Lý do |
|---|---|
| `webMode_cliDoesNotAutoApprove` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_submitSendsRiskLevel_andShowsServerAutoApproval` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_submitPrintsServerAutoApproval_insteadOfAskingDev` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_submitDesignRaise_submitsBeforeRiskEvent` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_resubmitBrdAfterRaise_sendsRiskLevel` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_casesRaiseThenMavenThrows_stillSendsRiskEvent` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_statusShowsSupersededGate` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |
| `webMode_failedSubmitStillReportsRiskLevel` | chế độ web: nộp/duyệt và event rủi ro qua FakeWeb |

### HookCommandTest (aisdlc-cli, 5)

| Test Java | Lý do |
|---|---|
| `postToolUseEmitsFileEditWithScope` | post-tool-use ghi outbox, WebPublisher, flush nền |
| `postToolUseBashOnlyWhenStepRunning` | post-tool-use ghi outbox, WebPublisher, flush nền |
| `postToolUseFastWhenWebDown` | post-tool-use ghi outbox, WebPublisher, flush nền |
| `postToolUseSpawnsBackgroundFlushUnlessBackoff` | post-tool-use ghi outbox, WebPublisher, flush nền |
| `postToolUseWithoutActiveFeatureEmitsUntaggedEditButNoBash` | post-tool-use ghi outbox, WebPublisher, flush nền |

### HookControlTest (aisdlc-cli, 11)

| Test Java | Lý do |
|---|---|
| `pausedDenySpawnsFlushThrottled` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `instructionDeliveredOnce` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `pendingAcceptAppliedByHook` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `scopeDeniedEvent` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `sessionIdOnEvents` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `userPromptSubmitDeliversOnceAndSpawnsFlush` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `userPromptSubmitSilentOutsideRepoAndWithoutControlDir` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `pausedDenyWaitsForInFlightFlushThatLiftsPause` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `pausedDenyStillDeniedWhenInFlightFlushKeepsPause` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `subagentToolCallDoesNotClaimInstructions` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |
| `heldControlLockDeniesWriteAndSkipsOtherHooks` | hook nhận lệnh điều khiển, ghi sổ, event và flush nền |

### HttpCiApprovalSourceTest (aisdlc-cli, 5)

| Test Java | Lý do |
|---|---|
| `parsesApprovalsAndSendsBearer` | nguồn approval CI qua HTTP |
| `parsesAutoChannelAndRule` | nguồn approval CI qua HTTP |
| `parsesPinned` | nguồn approval CI qua HTTP |
| `non200IsIoExceptionWithoutToken` | nguồn approval CI qua HTTP |
| `unreachableIsIoException` | nguồn approval CI qua HTTP |

### OutboxTest (aisdlc-cli, 4)

| Test Java | Lý do |
|---|---|
| `capsAt2000DroppingOldestEvents` | outbox sự kiện web |
| `snapshotReplacesPrevious` | outbox sự kiện web |
| `listsSnapshotsBeforeEventsAndHonoursMax` | outbox sự kiện web |
| `sizeOfMissingOutboxIsZeroAndCreatesNothing` | outbox sự kiện web |

### PolicyCommandTest (aisdlc-cli, 2)

| Test Java | Lý do |
|---|---|
| `pull_importsWhen404ThenCaches` | `policy pull` cần web client |
| `pull_noRepoFile_importsFallbackWithoutFlow` | `policy pull` cần web client |

### StatusControlTest (aisdlc-cli, 1)

| Test Java | Lý do |
|---|---|
| `pausedOfflineMessage` | cần refresh qua web và áp lệnh chờ qua ControlLedger |

### WebControlTest (aisdlc-cli, 5)

| Test Java | Lý do |
|---|---|
| `validEnvelopeWritesCache` | cần web sống (FakeWeb): làm mới cache điều khiển qua HTTP |
| `badSignatureKeepsOldCache` | cần web sống (FakeWeb): làm mới cache điều khiển qua HTTP |
| `olderIssuedAtDoesNotOverwrite` | cần web sống (FakeWeb): làm mới cache điều khiển qua HTTP |
| `inboxFlagFollowsUndeliveredInstructions` | cần web sống (FakeWeb): làm mới cache điều khiển qua HTTP |
| `networkErrorReturnsFalseAndStartsBackoff` | cần web sống (FakeWeb): làm mới cache điều khiển qua HTTP |

### WebGatesTest (aisdlc-cli, 27)

| Test Java | Lý do |
|---|---|
| `refreshWritesVerifiedCache` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `badSignatureKeepsOldCacheAndReturnsFalse` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `envelopeOfOtherFeatureIsNotCached` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `newFeedbackAppendedOnceAcrossTwoRefreshes` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `concurrentRefreshesAppendFeedbackOnce` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `olderEnvelopeArrivingLaterDoesNotOverwriteCache` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `afterOneNetworkFailureNoMoreRequestsInProcessAndBackoffStarts` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `migrateSendsOnlyMatchingLocalApprovalsAndSkipsReview` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `migrateFullFeatureSendsBrdDesignCasesSkipsReview` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `submitNetworkErrorAbortsAndWritesNothing` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `submitSendsSnapshotThenArtifactsAndUpdatesCache` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `webJsonTokenUsedForCliEvenWhenEnvTokenSetAndSubmitWarns` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `modeIssuedAtMonotonic` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `switchingConfigToWebReasksServerDespiteLocalCache` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `approveWithoutApproveTokenPrintsWebLink` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `approveConflictExitsOneWithServerMessage` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `approveWithClaudeCodeGrantSendsClaudeCodeChannel` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `approveSendsTerminalChannelAndCardFlags` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `rejectInWebModeGoesToWebAndFeedbackComesBack` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `approveNotWebModeConflictExitsTwo` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `unauthorizedAndWrongKindAreExplainedInVietnamese` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `forbiddenWithServerReasonKeepsReason` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `gateEnvelopeSayingLocalSwitchesCachedModeToLocal` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `refreshRepinsKeyEndorsedByPinnedKeyAndRetries` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `refreshRefusesKeyNotEndorsedByPinnedKey` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `pinFirstTimeWith600` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |
| `otherKeyNeedsEndorsementByPinnedKey` | cần web sống (FakeWeb): mode/refresh/nộp/duyệt/ghim khóa qua HTTP |

### WebLoginTest (aisdlc-cli, 8)

| Test Java | Lý do |
|---|---|
| `withoutTtyExitsThree` | `web login`/`web status` cần web client |
| `forbiddenTokenSaysIngestTokenNeeded` | `web login`/`web status` cần web client |
| `successWritesWebJson600AndPinsKey` | `web login`/`web status` cần web client |
| `rotatedKeyWithoutEndorsementIsRefused` | `web login`/`web status` cần web client |
| `approveTokenStored600` | `web login`/`web status` cần web client |
| `webJsonBeatsEnvToken` | `web login`/`web status` cần web client |
| `envTokenOnlyStillUsedButWarned` | `web login`/`web status` cần web client |
| `noWarningWithoutEnvToken` | `web login`/`web status` cần web client |

### WebPublisherTest (aisdlc-cli, 16)

| Test Java | Lý do |
|---|---|
| `batchIsVersion2AndSessionIdOnlyWhenPresent` | WebPublisher, outbox, flush, backoff |
| `disabledWritesNothing` | WebPublisher, outbox, flush, backoff |
| `serverDownKeepsQueueAndExitCodeUnchanged` | WebPublisher, outbox, flush, backoff |
| `flushSendsBacklogMax20` | WebPublisher, outbox, flush, backoff |
| `sendsBearerAndContract` | WebPublisher, outbox, flush, backoff |
| `rejected400MovesToRejected` | WebPublisher, outbox, flush, backoff |
| `serverErrorKeepsFiles` | WebPublisher, outbox, flush, backoff |
| `eventGoesToOutboxAndStdoutStaysClean` | WebPublisher, outbox, flush, backoff |
| `publisherErrorsGoToStderrOnly` | WebPublisher, outbox, flush, backoff |
| `eventOnlyQueuesThenSpawnsBackgroundFlush` | WebPublisher, outbox, flush, backoff |
| `eventDoesNotSpawnWhileFlushRunning` | WebPublisher, outbox, flush, backoff |
| `connectionFailureStartsBackoffThatCommandsAndHooksHonour` | WebPublisher, outbox, flush, backoff |
| `backoffExpires` | WebPublisher, outbox, flush, backoff |
| `drainSendsWholeOutboxInBatches` | WebPublisher, outbox, flush, backoff |
| `inlineFlushStopsAtOverallDeadlineOnSlowServer` | WebPublisher, outbox, flush, backoff |
| `webFlushCommandDrainsSilently` | WebPublisher, outbox, flush, backoff |

### CiAutoApprovalTest (aisdlc-core, 3)

| Test Java | Lý do |
|---|---|
| `webCi_autoApprovalMatchingFlow_pass` | eval --ci ở chế độ web (approval qua HTTP) |
| `webCi_autoApprovalNotMatching_fail` | eval --ci ở chế độ web (approval qua HTTP) |
| `webCi_manualApproval_noAutoCheck` | eval --ci ở chế độ web (approval qua HTTP) |

### EvalServiceTest (aisdlc-core, 15)

| Test Java | Lý do |
|---|---|
| `ciWarnsOnMissingOrDriftedApprovalOfOtherGates` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `serverPinnedLocalWinsOverConfigWeb` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `serverNotPinnedAndConfigWebFailsClosed` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `serverNotPinnedAndConfigLocalStaysLocal` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeMissingGateIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeHashMismatchIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeReviewTreeMismatchIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeAllApprovedAndMatchingPasses` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeIgnoresLocalGatesJson` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeUnreachableIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeWithoutSourceNamesVariables` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webReportedModeWebOverridesLocalConfig` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `sourceFailureIsHardFailEvenWhenConfigSaysLocal` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `sourceRuntimeFailureIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |
| `webModeFullFlowMissingDesignIsHardFail` | eval với nguồn approval web (HttpCiApprovalSource); cần cả harness giai đoạn 3 |

### FeedbackLogTest (aisdlc-core, 3)

| Test Java | Lý do |
|---|---|
| `lastEntryPerGate` | phản hồi từ web ghép vào feedback.md khi refresh |
| `rejectedKeepsComment` | phản hồi từ web ghép vào feedback.md khi refresh |
| `webIdMarkerIsNotPartOfComment` | phản hồi từ web ghép vào feedback.md khi refresh |

### RepoIdentityTest (aisdlc-core, 1)

| Test Java | Lý do |
|---|---|
| `localWhenNoRemoteAndOriginWhenPresent` | định danh repo cho web; phần chuẩn hóa remote đã có TestNormalizeRemote |

### SnapshotBuilderTest (aisdlc-core, 12)

| Test Java | Lý do |
|---|---|
| `gatesStepsCasesAndFeature` | snapshot tính năng gửi lên web |
| `autoApprovalCarriesRule` | snapshot tính năng gửi lên web |
| `stepDiffExcludesFeatureStateAndUnmanagedFiles` | snapshot tính năng gửi lên web |
| `stepDiffEmptyWhenOnlyStateFilesChanged` | snapshot tính năng gửi lên web |
| `openQuestionsFromBrd` | snapshot tính năng gửi lên web |
| `bigArtifactIsTruncatedByUtf8Bytes` | snapshot tính năng gửi lên web |
| `hugeDiffsAreCappedAndSnapshotFitsLimit` | snapshot tính năng gửi lên web |
| `dropsOldestDiffsWhenOverTotalLimit` | snapshot tính năng gửi lên web |
| `includesReviewCards` | snapshot tính năng gửi lên web |
| `treeOnlyWhenReviewOpen` | snapshot tính năng gửi lên web |
| `noTreeInLocalModeEvenWhenReviewOpen` | snapshot tính năng gửi lên web |
| `oversizedReviewCardsAreDroppedLast` | snapshot tính năng gửi lên web |

