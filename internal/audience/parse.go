package audience

import (
	"encoding/json"
	"errors"
	"fmt"
)

func Parse(data []byte) (Node, error) {
	var rn rawNode
	if err := json.Unmarshal(data, &rn); err != nil {
		return nil, err
	}
	return rn.toNode()
}

type rawNode struct {
	Logic      string            `json:"logic"`
	Conditions []json.RawMessage `json:"conditions"`
	Condition  json.RawMessage   `json:"condition"`
	Type       string            `json:"type"`

	// ProfileAttribute
	Field    string          `json:"field"`
	Operator string          `json:"operator"`
	Value    json.RawMessage `json:"value"`

	// EventHistory
	EventType      string `json:"event_type"`
	TimeWindowDays int    `json:"time_window_days"`
	MinCount       int    `json:"min_count"`

	// Consent
	Channel string `json:"channel"`
	Topic   string `json:"topic"`
	State   string `json:"state"`

	// Score
	Model     string `json:"model"`
	ScoreName string `json:"score_name"`
}

func (rn *rawNode) toNode() (Node, error) {
	if rn.Logic != "" {
		switch rn.Logic {
		case "and":
			var conds []Node
			for _, c := range rn.Conditions {
				n, err := Parse(c)
				if err != nil {
					return nil, err
				}
				conds = append(conds, n)
			}
			return &And{Conditions: conds}, nil
		case "or":
			var conds []Node
			for _, c := range rn.Conditions {
				n, err := Parse(c)
				if err != nil {
					return nil, err
				}
				conds = append(conds, n)
			}
			return &Or{Conditions: conds}, nil
		case "not":
			if len(rn.Condition) == 0 {
				return nil, errors.New("not condition requires condition field")
			}
			n, err := Parse(rn.Condition)
			if err != nil {
				return nil, err
			}
			return &Not{Condition: n}, nil
		default:
			return nil, fmt.Errorf("unknown logic operator: %s", rn.Logic)
		}
	}

	if rn.Type == "" {
		return nil, errors.New("condition type or logic operator is required")
	}

	switch rn.Type {
	case "profile_attribute":
		if rn.Field == "" {
			return nil, errors.New("profile_attribute condition requires field")
		}
		if rn.Operator == "" {
			return nil, errors.New("profile_attribute condition requires operator")
		}
		switch rn.Operator {
		case "equals", "contains", "in", "greater_than", "less_than":
		default:
			return nil, fmt.Errorf("unknown profile_attribute operator: %s", rn.Operator)
		}
		var val any
		if len(rn.Value) > 0 {
			if err := json.Unmarshal(rn.Value, &val); err != nil {
				return nil, err
			}
		}
		return &ProfileAttribute{
			Field:    rn.Field,
			Operator: rn.Operator,
			Value:    val,
		}, nil

	case "event_history":
		if rn.EventType == "" {
			return nil, errors.New("event_history condition requires event_type")
		}
		if rn.Operator == "" {
			return nil, errors.New("event_history condition requires operator")
		}
		if rn.Operator != "has_occurred" && rn.Operator != "has_not_occurred" {
			return nil, fmt.Errorf("unknown event_history operator: %s", rn.Operator)
		}
		if rn.TimeWindowDays < 0 {
			return nil, errors.New("time_window_days cannot be negative")
		}
		if rn.MinCount < 0 {
			return nil, errors.New("min_count cannot be negative")
		}
		return &EventHistory{
			EventType:      rn.EventType,
			Operator:       rn.Operator,
			TimeWindowDays: rn.TimeWindowDays,
			MinCount:       rn.MinCount,
		}, nil

	case "consent":
		if rn.Channel == "" {
			return nil, errors.New("consent condition requires channel")
		}
		if rn.Topic == "" {
			return nil, errors.New("consent condition requires topic")
		}
		if rn.State != "subscribed" && rn.State != "unsubscribed" {
			return nil, fmt.Errorf("unknown consent state: %s", rn.State)
		}
		return &Consent{
			Channel: rn.Channel,
			Topic:   rn.Topic,
			State:   rn.State,
		}, nil

	case "score":
		if rn.Model == "" {
			return nil, errors.New("score condition requires model")
		}
		if rn.ScoreName == "" {
			return nil, errors.New("score condition requires score_name")
		}
		if rn.Operator == "" {
			return nil, errors.New("score condition requires operator")
		}
		switch rn.Operator {
		case "greater_than", "less_than", "equals":
		default:
			return nil, fmt.Errorf("unknown score operator: %s", rn.Operator)
		}
		if len(rn.Value) == 0 {
			return nil, errors.New("score condition requires value")
		}
		var val float64
		if err := json.Unmarshal(rn.Value, &val); err != nil {
			return nil, err
		}
		return &Score{
			Model:     rn.Model,
			ScoreName: rn.ScoreName,
			Operator:  rn.Operator,
			Value:     val,
		}, nil

	case "company":
		if rn.Field == "" || rn.Operator == "" {
			return nil, errors.New("company condition requires field and operator")
		}
		switch rn.Operator {
		case "equals", "contains", "in":
		default:
			return nil, fmt.Errorf("unknown company operator: %s", rn.Operator)
		}
		var val any
		if len(rn.Value) > 0 {
			if err := json.Unmarshal(rn.Value, &val); err != nil {
				return nil, err
			}
		}
		return &Company{Field: rn.Field, Operator: rn.Operator, Value: val}, nil

	default:
		return nil, fmt.Errorf("unknown condition type: %s", rn.Type)
	}
}
