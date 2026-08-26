package domain

import (
	"testing"
	"time"
)

func TestWorkflowQuarantineAndReleaseRules(t *testing.T) {
	c := AcclimationCase{CaseID: "C1", StorageTemperatureC: -30, OpenedBy: "operator", State: Draft, Revision: 1}
	if e := c.ConfigureProtocol(AcclimationProtocol{ProtocolID: "P", Stages: []Stage{{Index: 0, TargetTemperatureC: -20, HoldMinutes: 5}}, MaxWarmingRateCPerHour: 120, ReadingIntervalMinutes: 5}, time.Now()); e != nil {
		t.Fatal(e)
	}
	_ = c.StartMonitoring()
	if e := c.AddReading(EnvironmentalReading{ReadingID: "R", CaseID: "C1", StageIndex: 0, ObservedAt: time.Now(), ChamberTemperatureC: 0, SpecimenTemperatureC: 0, RelativeHumidityPercent: 40}); e != nil || c.State != Quarantined {
		t.Fatalf("expected quarantine: %v %s", e, c.State)
	}
	if e := c.RecordDeviation(DeviationRecord{DeviationID: "D", TriggerReadingID: "R", ReasonCode: "TEMP", RootCause: "sensor", CorrectiveAction: "reset", RetestFromStage: 0}); e != nil {
		t.Fatal(e)
	}
	if e := c.AddRetestReading(EnvironmentalReading{ReadingID: "RR", CaseID: "C1", StageIndex: 0, ObservedAt: time.Now().Add(5 * time.Minute), ChamberTemperatureC: -20, SpecimenTemperatureC: -20, RelativeHumidityPercent: 40}); e != nil || c.State != MonitoringPassed {
		t.Fatalf("retest failed: %v %s", e, c.State)
	}
}

func TestNewCaseNormalizesAndRejectsInvalidRegistration(t *testing.T) {
	c, err := NewAcclimationCase("C", []SpecimenTube{{TubeID: " T2 ", Label: " L2 "}, {TubeID: "T1", Label: "L1"}}, -35, " operator ", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if c.Revision != 1 || c.State != Draft || c.OpenedBy != "operator" || c.SpecimenTubes[0].TubeID != "T1" {
		t.Fatalf("case not normalized: %+v", c)
	}
	if _, err = NewAcclimationCase("C2", []SpecimenTube{{TubeID: "T1", Label: "same"}, {TubeID: "T2", Label: " same "}}, -35, "operator", time.Now()); err == nil {
		t.Fatal("duplicate label was accepted")
	}
	if _, err = NewAcclimationCase("C3", []SpecimenTube{{TubeID: "T1", Label: "L1"}}, -300, "operator", time.Now()); err == nil {
		t.Fatal("temperature below absolute zero was accepted")
	}
}

func TestDraftRevisionPreviewAndAtomicReadingBatch(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c, err := NewAcclimationCase("C", []SpecimenTube{{TubeID: "T1", Label: "old"}}, -30, "operator", now)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.ReviseDraft([]SpecimenTube{{TubeID: " T2 ", Label: "new"}, {TubeID: "T1", Label: "fixed"}}, -30, "登记纠正", now); err != nil {
		t.Fatal(err)
	}
	if c.Revision != 2 || len(c.DraftRevisions) != 1 || c.SpecimenTubes[1].TubeID != "T2" {
		t.Fatalf("草稿修订结果错误: %+v", c)
	}
	before := c.Clone()
	protocol := AcclimationProtocol{ProtocolID: "P", Stages: []Stage{{Index: 0, TargetTemperatureC: -20, HoldMinutes: 10}}, MaxWarmingRateCPerHour: 120, ReadingIntervalMinutes: 5, TargetTemperatureC: -20}
	preview, err := c.PreviewProtocol(protocol, now)
	if err != nil {
		t.Fatal(err)
	}
	if preview.TotalCheckpointCount != 2 || preview.EstimatedCompletionAt != now.Add(10*time.Minute) || c.Revision != before.Revision || c.Protocol != nil {
		t.Fatalf("方案试算产生副作用或日程错误: %+v", preview)
	}
	if err = c.ConfigureProtocol(protocol, now); err != nil {
		t.Fatal(err)
	}
	startRevision := c.Revision
	err = c.AddReadingsBatch([]EnvironmentalReading{
		{ReadingID: "R1", CaseID: "C", StageIndex: 0, CheckpointSequence: 1, ObservedAt: now, ChamberTemperatureC: -20, SpecimenTemperatureC: -20, RelativeHumidityPercent: 40},
		{ReadingID: "R2", CaseID: "C", StageIndex: 0, CheckpointSequence: 2, ObservedAt: now.Add(5 * time.Minute), ChamberTemperatureC: -20, SpecimenTemperatureC: -20, RelativeHumidityPercent: 40},
	})
	if err != nil {
		t.Fatal(err)
	}
	if c.Revision != startRevision+1 || c.State != MonitoringPassed || len(c.Readings) != 2 {
		t.Fatalf("批量读数提交错误: %+v", c)
	}
}

func TestReadingBatchAndTubeEvidenceRejectWithoutPartialMutation(t *testing.T) {
	now := time.Now().UTC()
	c, _ := NewAcclimationCase("C", []SpecimenTube{{TubeID: "T1", Label: "L1"}, {TubeID: "T2", Label: "L2"}}, -20, "operator", now)
	protocol := AcclimationProtocol{ProtocolID: "P", Stages: []Stage{{Index: 0, TargetTemperatureC: -20, HoldMinutes: 10}}, MaxWarmingRateCPerHour: 10, ReadingIntervalMinutes: 5, TargetTemperatureC: -20}
	if err := c.ConfigureProtocol(protocol, now); err != nil {
		t.Fatal(err)
	}
	revision := c.Revision
	err := c.AddReadingsBatch([]EnvironmentalReading{
		{ReadingID: "R1", CaseID: "C", StageIndex: 0, CheckpointSequence: 1, ObservedAt: now, ChamberTemperatureC: -20, SpecimenTemperatureC: -20, RelativeHumidityPercent: 40},
		{ReadingID: "R2", CaseID: "C", StageIndex: 0, CheckpointSequence: 2, ObservedAt: now.Add(-time.Minute), ChamberTemperatureC: -20, SpecimenTemperatureC: -20, RelativeHumidityPercent: 40},
	})
	if err == nil || c.Revision != revision || len(c.Readings) != 0 {
		t.Fatalf("失败批量发生部分保存: err=%v case=%+v", err, c)
	}
	c.State = MonitoringPassed
	passed := true
	err = c.AddEvidence(ContaminationEvidence{BlankSampleID: "B", BlankSamplePassed: &passed, CollectedAt: now, TubeInspections: []TubeInspection{{TubeID: "T1", PackagingIntact: true}}}, now)
	if err == nil || c.Evidence != nil || c.Revision != revision {
		t.Fatalf("缺管证据被保存: err=%v case=%+v", err, c)
	}
}
