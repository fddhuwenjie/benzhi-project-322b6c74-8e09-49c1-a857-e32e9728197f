package domain

import (
	"sort"
	"strings"
	"time"
)

func (c *AcclimationCase) RecordDeviation(d DeviationRecord, clock ...time.Time) error {
	now := time.Now()
	if len(clock) > 0 {
		now = clock[0]
	}
	if c.State != Quarantined {
		return Invalid("批次未隔离")
	}
	d.DeviationID = strings.TrimSpace(d.DeviationID)
	d.TriggerReadingID = strings.TrimSpace(d.TriggerReadingID)
	d.ReasonCode = strings.TrimSpace(d.ReasonCode)
	d.RootCause = strings.TrimSpace(d.RootCause)
	d.CorrectiveAction = strings.TrimSpace(d.CorrectiveAction)
	if d.DeviationID == "" || d.TriggerReadingID == "" || d.ReasonCode == "" || d.RootCause == "" || d.CorrectiveAction == "" || d.RetestFromStage < 0 {
		return Invalid("偏差处置资料不完整")
	}
	if len(c.Readings) == 0 {
		return Invalid("偏差必须引用最近一次失败读数")
	}
	latest := c.Readings[len(c.Readings)-1]
	if latest.Verdict != "FAIL" || latest.ReadingID != d.TriggerReadingID {
		return Invalid("偏差必须引用最近一次失败读数")
	}
	if c.Protocol == nil || d.RetestFromStage >= len(c.Protocol.Stages) || d.RetestFromStage > latest.StageIndex {
		return Invalid("重测起始阶段无效")
	}
	for _, old := range c.DeviationHistory {
		if old.DeviationID == d.DeviationID {
			return Conflict("deviation_id 已存在")
		}
	}
	d.CaseID = c.CaseID
	d.RecordedAt = now.UTC()
	c.Deviation = &d
	c.DeviationHistory = append(c.DeviationHistory, d)
	c.RetestExpected = nil
	sequence := 1
	expectedAt := latest.ObservedAt
	for stageIndex := d.RetestFromStage; stageIndex < len(c.Protocol.Stages); stageIndex++ {
		for checkpoint := 0; checkpoint < c.Protocol.Stages[stageIndex].ExpectedCheckpoints; checkpoint++ {
			expectedAt = expectedAt.Add(time.Duration(c.Protocol.ReadingIntervalMinutes) * time.Minute)
			c.RetestExpected = append(c.RetestExpected, Checkpoint{StageIndex: stageIndex, Sequence: sequence, ExpectedAt: expectedAt})
			sequence++
		}
	}
	c.RetestReadings = nil
	c.Revision++
	return nil
}

func (c *AcclimationCase) AddEvidence(e ContaminationEvidence, clock ...time.Time) error {
	now := time.Now()
	if len(clock) > 0 {
		now = clock[0]
	}
	if c.State != MonitoringPassed && c.State != EvidenceReady {
		return Invalid("当前状态不允许提交污染证据")
	}
	e.BlankSampleID = strings.TrimSpace(e.BlankSampleID)
	if e.BlankSampleID == "" || e.ParticleCount < 0 || e.CollectedAt.IsZero() {
		return Invalid("污染证据字段不完整")
	}
	now = now.UTC()
	if e.CollectedAt.After(now) {
		return Invalid("证据采集时间不得晚于提交时间")
	}
	e.SubmittedAt = now
	e.Version = len(c.EvidenceVersions) + 1
	if c.Evidence != nil {
		e.PreviousVersion = c.Evidence.Version
	}
	e.FailureReasons, e.FailedTubeIDs = nil, nil
	if len(e.TubeInspections) > 0 {
		registered := make(map[string]struct{}, len(c.SpecimenTubes))
		for _, tube := range c.SpecimenTubes {
			registered[tube.TubeID] = struct{}{}
		}
		seen := make(map[string]struct{}, len(e.TubeInspections))
		totalParticles, allPackagingIntact := 0, true
		for i := range e.TubeInspections {
			inspection := &e.TubeInspections[i]
			inspection.TubeID = strings.TrimSpace(inspection.TubeID)
			if _, exists := registered[inspection.TubeID]; !exists {
				return Invalid("逐管证据包含未知 tube_id")
			}
			if _, exists := seen[inspection.TubeID]; exists {
				return Invalid("逐管证据 tube_id 重复")
			}
			if inspection.ParticleCount < 0 {
				return Invalid("逐管颗粒计数不得为负数")
			}
			seen[inspection.TubeID] = struct{}{}
			inspection.FailureReasons = nil
			if inspection.ParticleCount > 10 {
				inspection.FailureReasons = append(inspection.FailureReasons, "particle_count_exceeded")
			}
			if !inspection.PackagingIntact {
				inspection.FailureReasons = append(inspection.FailureReasons, "packaging_not_intact")
				allPackagingIntact = false
			}
			if len(inspection.FailureReasons) == 0 {
				inspection.Verdict = "PASS"
			} else {
				inspection.Verdict = "FAIL"
				e.FailedTubeIDs = append(e.FailedTubeIDs, inspection.TubeID)
			}
			totalParticles += inspection.ParticleCount
		}
		if len(seen) != len(registered) {
			return Invalid("逐管证据必须恰好覆盖当前批次全部 tube_id")
		}
		sort.Slice(e.TubeInspections, func(i, j int) bool { return e.TubeInspections[i].TubeID < e.TubeInspections[j].TubeID })
		sort.Strings(e.FailedTubeIDs)
		e.ParticleCount, e.PackagingIntact, e.CoverageCount = totalParticles, allPackagingIntact, len(seen)
		if e.BlankSamplePassed == nil {
			return Invalid("逐管证据必须提供 blank_sample_passed")
		}
		if !*e.BlankSamplePassed {
			e.FailureReasons = append(e.FailureReasons, "blank_sample_failed")
		}
		if len(e.FailedTubeIDs) > 0 {
			e.FailureReasons = append(e.FailureReasons, "tube_inspection_failed")
		}
	} else {
		if e.ParticleCount > 10 {
			e.FailureReasons = append(e.FailureReasons, "particle_count_exceeded")
		}
		if !e.PackagingIntact {
			e.FailureReasons = append(e.FailureReasons, "packaging_not_intact")
		}
	}
	if len(e.FailureReasons) == 0 {
		e.Conclusion = "PASS"
	} else {
		e.Conclusion = "FAIL"
	}
	c.EvidenceVersions = append(c.EvidenceVersions, EvidenceVersion{Version: e.Version, Evidence: e, Valid: e.Conclusion == "PASS"})
	c.Evidence = &e
	if e.Conclusion == "PASS" {
		c.State = EvidenceReady
	} else {
		c.State = MonitoringPassed
	}
	c.Revision++
	return nil
}

func (c *AcclimationCase) RespondToIssues(responderID string, responses []IssueResponse, now time.Time) error {
	if c.State != EvidenceReady {
		return Conflict("仅 EvidenceReady 批次可提交整改响应")
	}
	responderID = strings.TrimSpace(responderID)
	if responderID == "" || responderID != c.OpenedBy {
		return Invalid("整改响应人必须为批次经办人")
	}
	if len(responses) == 0 {
		return Invalid("问题响应列表不能为空")
	}
	outstanding := c.outstandingIssues()
	seen := make(map[string]struct{}, len(responses))
	validated := make([]IssueResponse, len(responses))
	for i, response := range responses {
		response.IssueID = strings.TrimSpace(response.IssueID)
		response.Response = strings.TrimSpace(response.Response)
		issue, exists := outstanding[response.IssueID]
		if !exists {
			return Invalid("响应的问题未知或已销项")
		}
		if _, duplicate := seen[response.IssueID]; duplicate {
			return Invalid("同一请求不能重复响应 issue_id")
		}
		for _, existing := range c.IssueResponses {
			if existing.IssueID == response.IssueID {
				return Conflict("该问题已有待复核整改响应")
			}
		}
		if response.Response == "" {
			return Invalid("整改说明不能为空")
		}
		evidence, valid := c.evidenceVersion(response.EvidenceVersion)
		if !valid || !evidence.Valid || response.EvidenceVersion <= issue.EvidenceVersion {
			return Invalid("整改必须关联问题提出后生成的合格证据版本")
		}
		seen[response.IssueID] = struct{}{}
		response.ResponderID, response.RespondedAt = responderID, now.UTC()
		validated[i] = response
	}
	sort.Slice(validated, func(i, j int) bool { return validated[i].IssueID < validated[j].IssueID })
	c.IssueResponses = append(c.IssueResponses, validated...)
	c.Revision++
	return nil
}

func (c AcclimationCase) evidenceVersion(version int) (EvidenceVersion, bool) {
	for _, candidate := range c.EvidenceVersions {
		if candidate.Version == version {
			return candidate, true
		}
	}
	return EvidenceVersion{}, false
}

func (c *AcclimationCase) SubmitReview(r Review, clock ...time.Time) error {
	now := time.Now()
	if len(clock) > 0 {
		now = clock[0]
	}
	if c.State != EvidenceReady {
		return Invalid("证据尚未就绪")
	}
	if len(r.Issues) > 0 {
		return Invalid("复核问题必须使用 structured_issues")
	}
	r.ReviewerID = strings.TrimSpace(r.ReviewerID)
	if r.ReviewerID == "" || r.ReviewerID == c.OpenedBy {
		return Invalid("复核员必须与经办人不同")
	}
	seen := make(map[string]struct{}, len(r.StructuredIssues))
	for i := range r.StructuredIssues {
		issue := &r.StructuredIssues[i]
		issue.IssueID = strings.TrimSpace(issue.IssueID)
		issue.Category = strings.TrimSpace(issue.Category)
		issue.Description = strings.TrimSpace(issue.Description)
		issue.Resolution = strings.TrimSpace(issue.Resolution)
		if issue.IssueID == "" || issue.Category == "" || issue.Description == "" || !c.hasEvidenceVersion(issue.EvidenceVersion) {
			return Invalid("复核问题字段或证据版本无效")
		}
		if _, exists := seen[issue.IssueID]; exists {
			return Invalid("复核问题 issue_id 重复")
		}
		seen[issue.IssueID] = struct{}{}
	}
	if !r.Approved {
		if len(r.StructuredIssues) == 0 {
			return Invalid("退回必须包含结构化复核问题")
		}
		unresolved := false
		for _, issue := range r.StructuredIssues {
			for _, previous := range c.ReviewHistory {
				for _, existing := range previous.StructuredIssues {
					if existing.IssueID == issue.IssueID {
						return Invalid("复核问题 issue_id 已存在")
					}
				}
			}
			if !issue.Resolved {
				unresolved = true
			}
		}
		if !unresolved {
			return Invalid("退回必须至少保留一个未解决问题")
		}
	} else {
		if c.Evidence == nil || c.Evidence.Conclusion != "PASS" {
			return Invalid("当前证据未通过")
		}
		outstanding := c.outstandingIssues()
		if len(outstanding) != len(r.StructuredIssues) {
			return Invalid("必须销项全部复核问题")
		}
		for _, issue := range r.StructuredIssues {
			if _, exists := outstanding[issue.IssueID]; !exists || !issue.Resolved || issue.Resolution == "" || c.Evidence == nil || issue.EvidenceVersion != c.Evidence.Version {
				return Invalid("仍有未销项复核问题")
			}
			responseFound := false
			for _, response := range c.IssueResponses {
				if response.IssueID == issue.IssueID && response.EvidenceVersion == issue.EvidenceVersion {
					responseFound = true
					break
				}
			}
			if !responseFound {
				return Invalid("销项问题缺少对应整改响应")
			}
		}
	}
	r.ReviewedAt = now.UTC()
	c.ReviewHistory = append(c.ReviewHistory, r)
	c.Review = &r
	c.Revision++
	if r.Approved {
		c.State = Reviewed
	}
	return nil
}

func (c AcclimationCase) hasEvidenceVersion(version int) bool {
	for _, candidate := range c.EvidenceVersions {
		if candidate.Version == version {
			return true
		}
	}
	return false
}

func (c AcclimationCase) outstandingIssues() map[string]ReviewIssue {
	issues := map[string]ReviewIssue{}
	for _, review := range c.ReviewHistory {
		for _, issue := range review.StructuredIssues {
			if issue.Resolved {
				delete(issues, issue.IssueID)
			} else {
				issues[issue.IssueID] = issue
			}
		}
	}
	return issues
}

func (c *AcclimationCase) AuthorizeRelease(authorizer string, clock ...time.Time) error {
	now := time.Now()
	if len(clock) > 0 {
		now = clock[0]
	}
	if c.State != Reviewed {
		return Invalid("批次未通过复核")
	}
	authorizer = strings.TrimSpace(authorizer)
	if authorizer == "" || c.Review == nil || authorizer == c.OpenedBy || authorizer == c.Review.ReviewerID {
		return Invalid("授权人必须与经办和复核身份分离")
	}
	now = now.UTC()
	c.ArchivedAt = &now
	c.State = ReleasedArchived
	c.Revision++
	return nil
}
