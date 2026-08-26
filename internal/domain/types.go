package domain

import "time"

type State string

const (
	Draft            State = "Draft"
	ProtocolReady    State = "ProtocolReady"
	Monitoring       State = "Monitoring"
	Quarantined      State = "Quarantined"
	MonitoringPassed State = "MonitoringPassed"
	EvidenceReady    State = "EvidenceReady"
	Reviewed         State = "Reviewed"
	ReleasedArchived State = "ReleasedArchived"
)

type SpecimenTube struct {
	TubeID string `json:"tube_id"`
	Label  string `json:"label"`
}
type FieldDifference struct {
	Field  string `json:"field"`
	Before any    `json:"before"`
	After  any    `json:"after"`
}
type DraftRevision struct {
	Revision  int               `json:"revision"`
	Reason    string            `json:"reason"`
	Changes   []FieldDifference `json:"changes"`
	ChangedAt time.Time         `json:"changed_at"`
}
type Stage struct {
	Index               int     `json:"index"`
	TargetTemperatureC  float64 `json:"target_temperature_c"`
	HoldMinutes         int     `json:"hold_minutes"`
	ExpectedCheckpoints int     `json:"expected_checkpoints,omitempty"`
}
type AcclimationProtocol struct {
	ProtocolID             string    `json:"protocol_id"`
	CaseID                 string    `json:"case_id"`
	Stages                 []Stage   `json:"stages"`
	MaxWarmingRateCPerHour float64   `json:"max_warming_rate_c_per_hour"`
	ReadingIntervalMinutes int       `json:"reading_interval_minutes"`
	TargetTemperatureC     float64   `json:"target_temperature_c"`
	ApprovedAt             time.Time `json:"approved_at"`
}
type EnvironmentalReading struct {
	ReadingID               string            `json:"reading_id"`
	CaseID                  string            `json:"case_id"`
	StageIndex              int               `json:"stage_index"`
	CheckpointSequence      int               `json:"checkpoint_sequence,omitempty"`
	Sequence                int               `json:"sequence,omitempty"`
	ObservedAt              time.Time         `json:"observed_at"`
	ChamberTemperatureC     float64           `json:"chamber_temperature_c"`
	SpecimenTemperatureC    float64           `json:"specimen_temperature_c"`
	RelativeHumidityPercent float64           `json:"relative_humidity_percent"`
	Verdict                 string            `json:"verdict"`
	QualityDetails          map[string]string `json:"quality_details,omitempty"`
	Retest                  bool              `json:"retest,omitempty"`
}
type DeviationRecord struct {
	DeviationID      string     `json:"deviation_id"`
	CaseID           string     `json:"case_id"`
	TriggerReadingID string     `json:"trigger_reading_id"`
	ReasonCode       string     `json:"reason_code"`
	RootCause        string     `json:"root_cause"`
	CorrectiveAction string     `json:"corrective_action"`
	RetestFromStage  int        `json:"retest_from_stage"`
	RecordedAt       time.Time  `json:"recorded_at"`
	ClosedAt         *time.Time `json:"closed_at,omitempty"`
}
type ContaminationEvidence struct {
	BlankSampleID     string           `json:"blank_sample_id"`
	ParticleCount     int              `json:"particle_count"`
	PackagingIntact   bool             `json:"packaging_intact"`
	Conclusion        string           `json:"conclusion"`
	SubmittedAt       time.Time        `json:"submitted_at"`
	CollectedAt       time.Time        `json:"collected_at,omitempty"`
	Version           int              `json:"version,omitempty"`
	PreviousVersion   int              `json:"previous_version,omitempty"`
	FailureReasons    []string         `json:"failure_reasons,omitempty"`
	BlankSamplePassed *bool            `json:"blank_sample_passed,omitempty"`
	TubeInspections   []TubeInspection `json:"tube_inspections,omitempty"`
	CoverageCount     int              `json:"coverage_count,omitempty"`
	FailedTubeIDs     []string         `json:"failed_tube_ids,omitempty"`
}
type TubeInspection struct {
	TubeID          string   `json:"tube_id"`
	ParticleCount   int      `json:"particle_count"`
	PackagingIntact bool     `json:"packaging_intact"`
	Verdict         string   `json:"verdict,omitempty"`
	FailureReasons  []string `json:"failure_reasons,omitempty"`
}
type EvidenceVersion struct {
	Version  int                   `json:"version"`
	Evidence ContaminationEvidence `json:"evidence"`
	Valid    bool                  `json:"valid"`
}
type ReviewIssue struct {
	IssueID         string `json:"issue_id"`
	Category        string `json:"category"`
	Description     string `json:"description"`
	EvidenceVersion int    `json:"evidence_version"`
	Resolved        bool   `json:"resolved"`
	Resolution      string `json:"resolution,omitempty"`
}
type IssueResponse struct {
	IssueID         string    `json:"issue_id"`
	Response        string    `json:"response"`
	EvidenceVersion int       `json:"evidence_version"`
	ResponderID     string    `json:"responder_id"`
	RespondedAt     time.Time `json:"responded_at"`
}
type Review struct {
	ReviewerID       string        `json:"reviewer_id"`
	Approved         bool          `json:"approved"`
	Issues           []string      `json:"issues,omitempty"`
	StructuredIssues []ReviewIssue `json:"structured_issues,omitempty"`
	ReviewedAt       time.Time     `json:"reviewed_at"`
}
type ReleaseArchive struct {
	ArchiveID           string    `json:"archive_id"`
	CaseID              string    `json:"case_id"`
	ReviewerID          string    `json:"reviewer_id"`
	ReleaseAuthorizerID string    `json:"release_authorizer_id"`
	ManifestEntries     []string  `json:"manifest_entries"`
	ManifestDigest      string    `json:"manifest_digest"`
	FinalRevision       int       `json:"final_revision"`
	SealedAt            time.Time `json:"sealed_at"`
}
type AcclimationCase struct {
	CaseID               string                 `json:"case_id"`
	SpecimenTubes        []SpecimenTube         `json:"specimen_tubes"`
	StorageTemperatureC  float64                `json:"storage_temperature_c"`
	State                State                  `json:"state"`
	Revision             int                    `json:"revision"`
	OpenedBy             string                 `json:"opened_by"`
	CreatedAt            time.Time              `json:"created_at"`
	ArchivedAt           *time.Time             `json:"archived_at,omitempty"`
	Protocol             *AcclimationProtocol   `json:"protocol,omitempty"`
	Readings             []EnvironmentalReading `json:"readings,omitempty"`
	Deviation            *DeviationRecord       `json:"deviation,omitempty"`
	DeviationHistory     []DeviationRecord      `json:"deviation_history,omitempty"`
	Evidence             *ContaminationEvidence `json:"evidence,omitempty"`
	EvidenceVersions     []EvidenceVersion      `json:"evidence_versions,omitempty"`
	Review               *Review                `json:"review,omitempty"`
	ReviewHistory        []Review               `json:"review_history,omitempty"`
	RetestReadings       []EnvironmentalReading `json:"retest_readings,omitempty"`
	RetestExpected       []Checkpoint           `json:"retest_expected,omitempty"`
	Archive              *ReleaseArchive        `json:"archive,omitempty"`
	DraftRevisions       []DraftRevision        `json:"draft_revisions,omitempty"`
	IssueResponses       []IssueResponse        `json:"issue_responses,omitempty"`
	LastError            string                 `json:"last_error,omitempty"`
	FirstFailedReadingID string                 `json:"first_failed_reading_id,omitempty"`
}

type Checkpoint struct {
	StageIndex int       `json:"stage_index"`
	Sequence   int       `json:"sequence"`
	ExpectedAt time.Time `json:"expected_at,omitempty"`
}

func (c *AcclimationCase) Clone() AcclimationCase {
	b, _ := jsonMarshal(c)
	var out AcclimationCase
	_ = jsonUnmarshal(b, &out)
	return out
}
