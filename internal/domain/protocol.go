package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
)

const maxProtocolCheckpoints = 100000

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

type ProtocolStageSchedule struct {
	StageIndex             int         `json:"stage_index"`
	TemperatureDifferenceC float64     `json:"temperature_difference_c"`
	MinimumWarmingMinutes  int         `json:"minimum_warming_minutes"`
	HoldEndsAt             time.Time   `json:"hold_ends_at"`
	CheckpointCount        int         `json:"checkpoint_count"`
	CheckpointTimes        []time.Time `json:"checkpoint_times"`
}

type ProtocolPreview struct {
	Stages                  []ProtocolStageSchedule `json:"stages"`
	TotalEstimatedMinutes   int                     `json:"total_estimated_minutes"`
	TotalCheckpointCount    int                     `json:"total_checkpoint_count"`
	FinalTargetTemperatureC float64                 `json:"final_target_temperature_c"`
	EstimatedCompletionAt   time.Time               `json:"estimated_completion_at"`
}

func (c AcclimationCase) PreviewProtocol(p AcclimationProtocol, startsAt time.Time) (ProtocolPreview, error) {
	if c.State != Draft {
		return ProtocolPreview{}, Conflict("仅 Draft 批次可试算方案")
	}
	if startsAt.IsZero() {
		return ProtocolPreview{}, Invalid("planned_start_at 不能为空")
	}
	normalized, err := c.validateProtocol(p)
	if err != nil {
		return ProtocolPreview{}, err
	}
	result := ProtocolPreview{Stages: []ProtocolStageSchedule{}, FinalTargetTemperatureC: normalized.TargetTemperatureC}
	cursor, previous := startsAt.UTC(), c.StorageTemperatureC
	for _, stage := range normalized.Stages {
		delta := stage.TargetTemperatureC - previous
		minimum := int(math.Ceil(delta / normalized.MaxWarmingRateCPerHour * 60))
		if minimum < 0 {
			minimum = 0
		}
		item := ProtocolStageSchedule{StageIndex: stage.Index, TemperatureDifferenceC: delta, MinimumWarmingMinutes: minimum, CheckpointCount: stage.ExpectedCheckpoints, CheckpointTimes: []time.Time{}}
		for sequence := 1; sequence <= stage.ExpectedCheckpoints; sequence++ {
			item.CheckpointTimes = append(item.CheckpointTimes, cursor.Add(time.Duration(sequence*normalized.ReadingIntervalMinutes)*time.Minute))
		}
		cursor = cursor.Add(time.Duration(stage.HoldMinutes) * time.Minute)
		item.HoldEndsAt = cursor
		result.TotalCheckpointCount += item.CheckpointCount
		result.TotalEstimatedMinutes += stage.HoldMinutes
		result.Stages = append(result.Stages, item)
		previous = stage.TargetTemperatureC
	}
	result.EstimatedCompletionAt = cursor
	return result, nil
}

func (c AcclimationCase) validateProtocol(p AcclimationProtocol) (AcclimationProtocol, error) {
	if strings.TrimSpace(p.ProtocolID) == "" || len(p.Stages) == 0 || !finite(p.MaxWarmingRateCPerHour) || p.MaxWarmingRateCPerHour <= 0 || p.ReadingIntervalMinutes <= 0 || !finite(p.TargetTemperatureC) {
		return p, Err("PROTOCOL_FIELDS_INVALID", "方案参数不完整")
	}
	for i, stage := range p.Stages {
		if stage.Index != i {
			return p, Err("STAGE_INDEX_INVALID", fmt.Sprintf("stage %d 必须连续编号", i))
		}
		if stage.HoldMinutes <= 0 || stage.HoldMinutes%p.ReadingIntervalMinutes != 0 || !finite(stage.TargetTemperatureC) {
			return p, Err("STAGE_WINDOW_INVALID", fmt.Sprintf("stage %d 保持时长或读数频率无效", i))
		}
		if i > 0 && stage.TargetTemperatureC < p.Stages[i-1].TargetTemperatureC {
			return p, Err("STAGE_NOT_MONOTONIC", fmt.Sprintf("stage %d 温度必须单调升高", i))
		}
	}
	if p.TargetTemperatureC == 0 {
		p.TargetTemperatureC = p.Stages[len(p.Stages)-1].TargetTemperatureC
	}
	if p.Stages[len(p.Stages)-1].TargetTemperatureC != p.TargetTemperatureC {
		return p, Err("FINAL_TARGET_MISMATCH", "最终阶段温度必须等于目标温度")
	}
	if p.Stages[0].TargetTemperatureC < c.StorageTemperatureC {
		return p, Err("STAGE_BELOW_STORAGE", "stage 0 温度不得低于保存温度")
	}
	previous, total := c.StorageTemperatureC, 0
	for i := range p.Stages {
		delta := p.Stages[i].TargetTemperatureC - previous
		minimum := int(math.Ceil(delta / p.MaxWarmingRateCPerHour * 60))
		if delta > p.MaxWarmingRateCPerHour*float64(p.Stages[i].HoldMinutes)/60+1e-9 {
			return p, Err("STAGE_WARMING_RATE_EXCEEDED", fmt.Sprintf("stage %d 最低可行升温时长为 %d 分钟", i, minimum))
		}
		p.Stages[i].ExpectedCheckpoints = p.Stages[i].HoldMinutes / p.ReadingIntervalMinutes
		total += p.Stages[i].ExpectedCheckpoints
		if p.Stages[i].ExpectedCheckpoints <= 0 || total > maxProtocolCheckpoints {
			return p, Err("CHECKPOINT_LIMIT_EXCEEDED", fmt.Sprintf("stage %d 检查点数量无效", i))
		}
		previous = p.Stages[i].TargetTemperatureC
	}
	return p, nil
}

func (c *AcclimationCase) ConfigureProtocol(p AcclimationProtocol, now time.Time) error {
	if c.State != Draft {
		return Invalid("仅 Draft 批次可配置方案")
	}
	normalized, err := c.validateProtocol(p)
	if err != nil {
		return err
	}
	normalized.CaseID, normalized.ApprovedAt = c.CaseID, now.UTC()
	c.Protocol = &normalized
	c.State = ProtocolReady
	c.Revision++
	return nil
}

func (c *AcclimationCase) StartMonitoring() error {
	if c.State != ProtocolReady {
		return Invalid("方案未就绪")
	}
	c.State = Monitoring
	c.Revision++
	return nil
}
