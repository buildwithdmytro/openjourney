package journey

import "encoding/json"

type Graph struct {
	EntryNodeID string `json:"entry_node_id"`
	Nodes       []Node `json:"nodes"`
	Edges       []Edge `json:"edges"`
}

type Node struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Config json.RawMessage `json:"config"`
}

type Edge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Branch string `json:"branch,omitempty"`
}
