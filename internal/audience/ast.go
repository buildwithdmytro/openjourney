package audience


type Node interface {
	Type() string
}

type And struct {
	Conditions []Node `json:"conditions"`
}

func (a *And) Type() string { return "and" }

type Or struct {
	Conditions []Node `json:"conditions"`
}

func (o *Or) Type() string { return "or" }

type Not struct {
	Condition Node `json:"condition"`
}

func (n *Not) Type() string { return "not" }

type ProfileAttribute struct {
	Field    string `json:"field"`
	Operator string `json:"operator"` // equals, contains, in, greater_than, less_than
	Value    any    `json:"value"`
}

func (p *ProfileAttribute) Type() string { return "profile_attribute" }

type EventHistory struct {
	EventType      string `json:"event_type"`
	Operator       string `json:"operator"` // has_occurred, has_not_occurred
	TimeWindowDays int    `json:"time_window_days"`
	MinCount       int    `json:"min_count"`
}

func (e *EventHistory) Type() string { return "event_history" }

type Consent struct {
	Channel string `json:"channel"`
	Topic   string `json:"topic"`
	State   string `json:"state"` // subscribed, unsubscribed
}

func (c *Consent) Type() string { return "consent" }
