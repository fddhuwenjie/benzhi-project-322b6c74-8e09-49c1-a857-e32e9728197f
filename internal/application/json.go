package application

import (
	"encoding/json"
	"fmt"
)

func (c *CreateCommand) UnmarshalJSON(data []byte) error {
	type commandAlias CreateCommand
	var decoded commandAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	allowed := map[string]bool{"specimen_tubes": true, "storage_temperature_c": true, "opened_by": true, "request_id": true}
	for field := range fields {
		if !allowed[field] {
			return fmt.Errorf("未知 JSON 字段: %s", field)
		}
	}
	*c = CreateCommand(decoded)
	_, c.StorageProvided = fields["storage_temperature_c"]
	return nil
}
