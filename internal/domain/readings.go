package domain

import (
	"math"
	"strings"
	"time"
)

type StageProgress struct {
	StageIndex           int    `json:"stage_index"`
	Expected             int    `json:"expected"`
	Received             int    `json:"received"`
	Passed               int    `json:"passed"`
	Missing              int    `json:"missing"`
	FirstFailedReadingID string `json:"first_failed_reading_id,omitempty"`
}

type MonitoringProgress struct {
	Stages                   []StageProgress `json:"stages"`
	Expected                 int             `json:"expected"`
	Received                 int             `json:"received"`
	Passed                   int             `json:"passed"`
	Missing                  int             `json:"missing"`
	FirstFailedReadingID     string          `json:"first_failed_reading_id,omitempty"`
	NextStageIndex           *int            `json:"next_stage_index,omitempty"`
	NextCheckpointSequence   *int            `json:"next_checkpoint_sequence,omitempty"`
	NextCheckpointEarliestAt *time.Time      `json:"next_checkpoint_earliest_at,omitempty"`
	NextCheckpointLatestAt   *time.Time      `json:"next_checkpoint_latest_at,omitempty"`
	MonitoringComplete       bool            `json:"monitoring_complete"`
}

func (c *AcclimationCase) AddReading(r EnvironmentalReading) error {
	return c.addReading(r, false)
}

func (c *AcclimationCase) AddRetestReading(r EnvironmentalReading) error {
	return c.addReading(r, true)
}

func (c *AcclimationCase) AddReadingsBatch(readings []EnvironmentalReading) error {
	if len(readings) < 2 || len(readings) > 100 {
		return Invalid("批量读数必须包含 2 到 100 条")
	}
	working := c.Clone()
	startRevision := c.Revision
	seen := make(map[string]struct{}, len(readings))
	for index, reading := range readings {
		reading.ReadingID = strings.TrimSpace(reading.ReadingID)
		if _, exists := seen[reading.ReadingID]; exists {
			return Conflict("批量 reading_id 重复")
		}
		seen[reading.ReadingID] = struct{}{}
		if err := working.AddReading(reading); err != nil {
			return err
		}
		if working.Readings[len(working.Readings)-1].Verdict == "FAIL" && index != len(readings)-1 {
			return Invalid("超限读数只能位于批量载荷末尾")
		}
	}
	working.Revision = startRevision + 1
	*c = working
	return nil
}

func (c *AcclimationCase) addReading(r EnvironmentalReading, retest bool) error {
	if retest {
		if c.State != Quarantined || c.Deviation == nil || len(c.RetestExpected) == 0 {
			return Invalid("当前没有待执行的重测队列")
		}
	} else if c.State != ProtocolReady && c.State != Monitoring {
		return Invalid("当前状态不接收监测读数")
	}
	r.ReadingID = strings.TrimSpace(r.ReadingID)
	r.CaseID = strings.TrimSpace(r.CaseID)
	r.Retest = retest
	if c.Protocol == nil || r.CaseID != c.CaseID || r.ReadingID == "" || r.ObservedAt.IsZero() || r.StageIndex < 0 || r.StageIndex >= len(c.Protocol.Stages) {
		return Invalid("读数标识、批次、时间或阶段无效")
	}
	if !finite(r.ChamberTemperatureC) || !finite(r.SpecimenTemperatureC) || !finite(r.RelativeHumidityPercent) {
		return Invalid("读数必须为有限数值")
	}
	if r.RelativeHumidityPercent < 0 || r.RelativeHumidityPercent > 100 {
		return Invalid("相对湿度必须在 0 到 100 之间")
	}
	for _, existing := range c.Readings {
		if existing.ReadingID == r.ReadingID {
			return Conflict("reading_id 已存在")
		}
	}
	if len(c.Readings) > 0 {
		last := c.Readings[len(c.Readings)-1]
		elapsed := r.ObservedAt.Sub(last.ObservedAt)
		interval := time.Duration(c.Protocol.ReadingIntervalMinutes) * time.Minute
		if elapsed <= 0 {
			return Invalid("读数时间必须严格递增")
		}
		if elapsed < interval {
			return Invalid("读数早于允许检查点时间")
		}
		if elapsed > 2*interval {
			return Invalid("读数超过允许检查点时间窗")
		}
	}
	if retest {
		if len(c.RetestReadings) >= len(c.RetestExpected) {
			return Invalid("重测队列已经完成")
		}
		next := c.RetestExpected[len(c.RetestReadings)]
		sequence, err := readingSequence(r)
		if err != nil {
			return err
		}
		if sequence == 0 {
			sequence = next.Sequence
		}
		if next.StageIndex != r.StageIndex || sequence != next.Sequence {
			return Invalid("重测必须按阶段和序号提交")
		}
		r.CheckpointSequence, r.Sequence = sequence, 0
	} else if err := c.validateMonitoringCheckpoint(r.StageIndex); err != nil {
		return err
	} else {
		sequence, sequenceErr := readingSequence(r)
		if sequenceErr != nil {
			return sequenceErr
		}
		expectedSequence := 1
		for _, existing := range c.Readings {
			if !existing.Retest && existing.StageIndex == r.StageIndex {
				expectedSequence++
			}
		}
		if sequence != 0 && sequence != expectedSequence {
			return Invalid("监测读数必须按检查点序号提交")
		}
		r.CheckpointSequence, r.Sequence = expectedSequence, 0
	}

	r.Verdict, r.QualityDetails = c.quality(r)
	c.Readings = append(c.Readings, r)
	if retest {
		c.RetestReadings = append(c.RetestReadings, r)
	} else if c.State == ProtocolReady {
		c.State = Monitoring
	}
	c.Revision++
	if r.Verdict == "FAIL" {
		c.State = Quarantined
		c.LastError = "environmental reading exceeded threshold"
		if c.FirstFailedReadingID == "" {
			c.FirstFailedReadingID = r.ReadingID
		}
		return nil
	}
	if retest && len(c.RetestReadings) == len(c.RetestExpected) {
		c.State = MonitoringPassed
		c.LastError = ""
		now := time.Now().UTC()
		c.Deviation.ClosedAt = &now
		if len(c.DeviationHistory) > 0 {
			c.DeviationHistory[len(c.DeviationHistory)-1].ClosedAt = &now
		}
	} else if !retest && monitoringComplete(*c) {
		c.State = MonitoringPassed
	}
	return nil
}

func readingSequence(r EnvironmentalReading) (int, error) {
	if r.CheckpointSequence < 0 || r.Sequence < 0 || r.CheckpointSequence > 0 && r.Sequence > 0 && r.CheckpointSequence != r.Sequence {
		return 0, Invalid("检查点序号无效")
	}
	if r.CheckpointSequence > 0 {
		return r.CheckpointSequence, nil
	}
	return r.Sequence, nil
}

func (c AcclimationCase) validateMonitoringCheckpoint(stageIndex int) error {
	for i, stage := range c.Protocol.Stages {
		received := 0
		for _, reading := range c.Readings {
			if !reading.Retest && reading.StageIndex == i {
				received++
			}
		}
		if i < stageIndex && received < stage.ExpectedCheckpoints {
			return Invalid("前序阶段检查点尚未完成")
		}
		if i == stageIndex && received >= stage.ExpectedCheckpoints {
			return Invalid("该阶段检查点已完成")
		}
		if i > stageIndex && received > 0 {
			return Invalid("读数阶段不可回退")
		}
	}
	return nil
}

func (c AcclimationCase) quality(r EnvironmentalReading) (string, map[string]string) {
	stage := c.Protocol.Stages[r.StageIndex]
	details := map[string]string{
		"chamber_temperature":  "PASS",
		"specimen_temperature": "PASS",
		"humidity":             "PASS",
		"warming_rate":         "PASS",
	}
	if math.Abs(r.ChamberTemperatureC-stage.TargetTemperatureC) > 0.5 {
		details["chamber_temperature"] = "FAIL"
	}
	if math.Abs(r.SpecimenTemperatureC-stage.TargetTemperatureC) > 0.5 {
		details["specimen_temperature"] = "FAIL"
	}
	if r.RelativeHumidityPercent > 95 {
		details["humidity"] = "FAIL"
	}
	if len(c.Readings) > 0 {
		last := c.Readings[len(c.Readings)-1]
		hours := r.ObservedAt.Sub(last.ObservedAt).Hours()
		if hours > 0 && (r.ChamberTemperatureC-last.ChamberTemperatureC)/hours > c.Protocol.MaxWarmingRateCPerHour+1e-9 {
			details["warming_rate"] = "FAIL"
		}
	}
	for _, verdict := range details {
		if verdict == "FAIL" {
			return "FAIL", details
		}
	}
	return "PASS", details
}

func monitoringComplete(c AcclimationCase) bool {
	progress := c.MonitoringProgress()
	return progress.MonitoringComplete
}

func (c AcclimationCase) MonitoringProgress() MonitoringProgress {
	progress := MonitoringProgress{Stages: []StageProgress{}}
	if c.Protocol == nil {
		return progress
	}
	for i, stage := range c.Protocol.Stages {
		item := StageProgress{StageIndex: i, Expected: stage.ExpectedCheckpoints}
		useRetest := c.Deviation != nil && len(c.RetestExpected) > 0 && i >= c.Deviation.RetestFromStage
		if useRetest {
			item.Expected = 0
			for _, checkpoint := range c.RetestExpected {
				if checkpoint.StageIndex == i {
					item.Expected++
				}
			}
		}
		for _, reading := range c.Readings {
			if reading.StageIndex != i || reading.Retest != useRetest {
				continue
			}
			item.Received++
			if reading.Verdict == "PASS" {
				item.Passed++
			} else if item.FirstFailedReadingID == "" {
				item.FirstFailedReadingID = reading.ReadingID
			}
		}
		if useRetest && item.FirstFailedReadingID == "" {
			for _, reading := range c.Readings {
				if reading.StageIndex == i && reading.Verdict == "FAIL" {
					item.FirstFailedReadingID = reading.ReadingID
					break
				}
			}
		}
		item.Missing = item.Expected - item.Received
		if item.Missing < 0 {
			item.Missing = 0
		}
		progress.Stages = append(progress.Stages, item)
		progress.Expected += item.Expected
		progress.Received += item.Received
		progress.Passed += item.Passed
		progress.Missing += item.Missing
		if progress.FirstFailedReadingID == "" && item.FirstFailedReadingID != "" {
			progress.FirstFailedReadingID = item.FirstFailedReadingID
		}
		if progress.NextStageIndex == nil && item.Missing > 0 {
			stageIndex, sequence := i, item.Received+1
			progress.NextStageIndex = &stageIndex
			progress.NextCheckpointSequence = &sequence
		}
	}
	progress.MonitoringComplete = progress.Expected > 0 && progress.Missing == 0 && progress.Passed == progress.Expected
	if progress.NextStageIndex != nil && len(c.Readings) > 0 {
		earliest := c.Readings[len(c.Readings)-1].ObservedAt.Add(time.Duration(c.Protocol.ReadingIntervalMinutes) * time.Minute)
		latest := earliest.Add(time.Duration(c.Protocol.ReadingIntervalMinutes) * time.Minute)
		progress.NextCheckpointEarliestAt = &earliest
		progress.NextCheckpointLatestAt = &latest
	}
	return progress
}
