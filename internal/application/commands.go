package application

import "icecoreacclimationgate/internal/domain"

type CreateCommand struct {
	Tubes               []domain.SpecimenTube `json:"specimen_tubes"`
	StorageTemperatureC float64               `json:"storage_temperature_c"`
	OpenedBy            string                `json:"opened_by"`
	RequestID           string                `json:"request_id"`
	StorageProvided     bool                  `json:"-"`
}
