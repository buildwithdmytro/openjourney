package ai

import (
	"encoding/json"
	"fmt"
)

const (
	dataStart = "<untrusted_retrieved_data>"
	dataEnd   = "</untrusted_retrieved_data>"
)

// GovernedPrompt keeps retrieved values out of the instruction text. The data
// is encoded as JSON and placed in a clearly delimited section so a value
// retrieved from a tenant's data cannot become a prompt instruction.
func GovernedPrompt(instruction string, retrieved any) (string, error) {
	data, err := json.Marshal(retrieved)
	if err != nil {
		return "", fmt.Errorf("marshal retrieved data: %w", err)
	}
	return instruction + "\n\n" + dataStart + "\n" + string(data) + "\n" + dataEnd, nil
}
