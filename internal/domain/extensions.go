package domain

import (
	"sort"
	"strings"
	"time"
)

type RetestOption struct {
	RetestFromStage int `json:"retest_from_stage"`
	CheckpointCount int `json:"checkpoint_count"`
}

type RetestStageProgress struct {
	StageIndex             int        `json:"stage_index"`
	Expected               int        `json:"expected"`
	Received               int        `json:"received"`
	Passed                 int        `json:"passed"`
	Failed                 int        `json:"failed"`
	Missing                int        `json:"missing"`
	NextCheckpointSequence *int       `json:"next_checkpoint_sequence,omitempty"`
	NextEarliestAt         *time.Time `json:"next_earliest_at,omitempty"`
	NextLatestAt           *time.Time `json:"next_latest_at,omitempty"`
}

type QuarantineProgress struct {
	TriggerReading       EnvironmentalReading  `json:"trigger_reading"`
	FailedQualityItems   []string              `json:"failed_quality_items"`
	QuarantinedFromStage int                   `json:"quarantined_from_stage"`
	Deviation            *DeviationRecord      `json:"deviation,omitempty"`
	RootCauseComplete    bool                  `json:"root_cause_complete"`
	CorrectiveComplete   bool                  `json:"corrective_action_complete"`
	RetestOptions        []RetestOption        `json:"retest_options,omitempty"`
	Stages               []RetestStageProgress `json:"stages,omitempty"`
}

func (c AcclimationCase) QuarantineProgress() (QuarantineProgress, error) {
	if c.State != Quarantined {
		return QuarantineProgress{}, Conflict("批次不处于 Quarantined 状态")
	}
	var trigger EnvironmentalReading
	if len(c.Readings) > 0 && c.Readings[len(c.Readings)-1].Verdict == "FAIL" {
		trigger = c.Readings[len(c.Readings)-1]
	}
	for _, reading := range c.Readings {
		if trigger.ReadingID == "" && reading.ReadingID == c.FirstFailedReadingID {
			trigger = reading
			break
		}
	}
	if trigger.ReadingID == "" {
		for i := len(c.Readings) - 1; i >= 0; i-- {
			if c.Readings[i].Verdict == "FAIL" {
				trigger = c.Readings[i]
				break
			}
		}
	}
	result := QuarantineProgress{TriggerReading: trigger, QuarantinedFromStage: trigger.StageIndex, Deviation: c.Deviation, FailedQualityItems: []string{}}
	for item, verdict := range trigger.QualityDetails {
		if verdict == "FAIL" {
			result.FailedQualityItems = append(result.FailedQualityItems, item)
		}
	}
	sort.Strings(result.FailedQualityItems)
	if c.Deviation == nil {
		result.RetestOptions = []RetestOption{}
		for from := 0; from <= trigger.StageIndex; from++ {
			count := 0
			for stage := from; stage < len(c.Protocol.Stages); stage++ {
				count += c.Protocol.Stages[stage].ExpectedCheckpoints
			}
			result.RetestOptions = append(result.RetestOptions, RetestOption{RetestFromStage: from, CheckpointCount: count})
		}
		return result, nil
	}
	result.RootCauseComplete = strings.TrimSpace(c.Deviation.RootCause) != ""
	result.CorrectiveComplete = strings.TrimSpace(c.Deviation.CorrectiveAction) != ""
	result.Stages = []RetestStageProgress{}
	nextAssigned := false
	for stageIndex := c.Deviation.RetestFromStage; stageIndex < len(c.Protocol.Stages); stageIndex++ {
		item := RetestStageProgress{StageIndex: stageIndex}
		for _, checkpoint := range c.RetestExpected {
			if checkpoint.StageIndex == stageIndex {
				item.Expected++
			}
		}
		for _, reading := range c.RetestReadings {
			if reading.StageIndex != stageIndex {
				continue
			}
			item.Received++
			if reading.Verdict == "PASS" {
				item.Passed++
			} else {
				item.Failed++
			}
		}
		item.Missing = item.Expected - item.Received
		if item.Missing > 0 && !nextAssigned {
			sequence := len(c.RetestReadings) + 1
			item.NextCheckpointSequence = &sequence
			if len(c.RetestExpected) >= sequence {
				expected := c.RetestExpected[sequence-1].ExpectedAt
				earliest := expected
				latest := expected.Add(time.Duration(c.Protocol.ReadingIntervalMinutes) * time.Minute)
				item.NextEarliestAt, item.NextLatestAt = &earliest, &latest
			}
			nextAssigned = true
		}
		result.Stages = append(result.Stages, item)
	}
	return result, nil
}

type ReleaseCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Reason string `json:"reason,omitempty"`
}

func (c AcclimationCase) ReleaseChecks(authorizer string, expectedRevision int) []ReleaseCheck {
	authorizer = strings.TrimSpace(authorizer)
	monitoringCompleted := c.MonitoringProgress().MonitoringComplete
	checks := []ReleaseCheck{
		{Name: "revision_matches", Passed: expectedRevision == c.Revision, Reason: "expected_revision 与当前 revision 不一致"},
		{Name: "state_reviewed", Passed: c.State == Reviewed, Reason: "批次尚未处于 Reviewed"},
		{Name: "authorizer_present", Passed: authorizer != "", Reason: "authorizer_id 不能为空"},
		{Name: "authorizer_separated", Passed: authorizer != "" && authorizer != c.OpenedBy && c.Review != nil && authorizer != c.Review.ReviewerID, Reason: "授权人必须与经办人及最终复核员不同"},
		{Name: "monitoring_completed", Passed: monitoringCompleted, Reason: "监测尚未完成"},
		{Name: "deviation_closed", Passed: c.Deviation == nil || c.Deviation.ClosedAt != nil, Reason: "活动偏差尚未关闭"},
		{Name: "latest_evidence_passed", Passed: c.Evidence != nil && c.Evidence.Conclusion == "PASS", Reason: "最新污染证据未通过"},
		{Name: "review_issues_resolved", Passed: len(c.outstandingIssues()) == 0, Reason: "仍有结构化复核问题未销项"},
		{Name: "final_review_approved", Passed: c.Review != nil && c.Review.Approved, Reason: "最终复核尚未批准"},
	}
	for i := range checks {
		if checks[i].Passed {
			checks[i].Reason = ""
		}
	}
	return checks
}
