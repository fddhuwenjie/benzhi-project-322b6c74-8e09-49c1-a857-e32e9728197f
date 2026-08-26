package domain

import (
	"math"
	"sort"
	"strings"
	"time"
)

const AbsoluteZeroC = -273.15

func NewAcclimationCase(id string, tubes []SpecimenTube, storage float64, openedBy string, createdAt time.Time) (AcclimationCase, error) {
	openedBy = strings.TrimSpace(openedBy)
	if strings.TrimSpace(id) == "" || openedBy == "" || len(tubes) == 0 {
		return AcclimationCase{}, Invalid("登记信息不完整")
	}
	normalized, err := normalizeDraft(tubes, storage)
	if err != nil {
		return AcclimationCase{}, err
	}
	return AcclimationCase{
		CaseID: id, SpecimenTubes: normalized, StorageTemperatureC: storage,
		State: Draft, Revision: 1, OpenedBy: openedBy, CreatedAt: createdAt.UTC(),
	}, nil
}

func normalizeDraft(tubes []SpecimenTube, storage float64) ([]SpecimenTube, error) {
	if len(tubes) == 0 {
		return nil, Invalid("冰芯管清单不能为空")
	}
	if math.IsNaN(storage) || math.IsInf(storage, 0) || storage < AbsoluteZeroC {
		return nil, Invalid("保存温度必须为不低于绝对零度的有限数值")
	}
	normalized := make([]SpecimenTube, len(tubes))
	tubeIDs := make(map[string]struct{}, len(tubes))
	labels := make(map[string]struct{}, len(tubes))
	for i, tube := range tubes {
		tube.TubeID = strings.TrimSpace(tube.TubeID)
		tube.Label = strings.TrimSpace(tube.Label)
		if tube.TubeID == "" || tube.Label == "" {
			return nil, Invalid("冰芯管编号和标签不能为空")
		}
		if _, exists := tubeIDs[tube.TubeID]; exists {
			return nil, Invalid("冰芯管编号重复")
		}
		if _, exists := labels[tube.Label]; exists {
			return nil, Invalid("冰芯管标签重复")
		}
		tubeIDs[tube.TubeID] = struct{}{}
		labels[tube.Label] = struct{}{}
		normalized[i] = tube
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].TubeID < normalized[j].TubeID })
	return normalized, nil
}

func (c *AcclimationCase) ReviseDraft(tubes []SpecimenTube, storage float64, reason string, now time.Time) error {
	if c.State != Draft {
		return Conflict("仅 Draft 批次可修订登记清单")
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Invalid("变更原因不能为空")
	}
	normalized, err := normalizeDraft(tubes, storage)
	if err != nil {
		return err
	}
	changes := make([]FieldDifference, 0, 2)
	if !sameTubes(c.SpecimenTubes, normalized) {
		changes = append(changes, FieldDifference{Field: "specimen_tubes", Before: c.SpecimenTubes, After: normalized})
	}
	if c.StorageTemperatureC != storage {
		changes = append(changes, FieldDifference{Field: "storage_temperature_c", Before: c.StorageTemperatureC, After: storage})
	}
	if len(changes) == 0 {
		return Conflict("修订内容没有实际变化")
	}
	c.SpecimenTubes = normalized
	c.StorageTemperatureC = storage
	c.Revision++
	c.DraftRevisions = append(c.DraftRevisions, DraftRevision{Revision: c.Revision, Reason: reason, Changes: changes, ChangedAt: now.UTC()})
	return nil
}

func sameTubes(a, b []SpecimenTube) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
