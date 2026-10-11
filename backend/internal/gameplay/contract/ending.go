package contract

// EndingCapability is public information, never model execution authority.
type EndingCapability struct {
	Key            string         `json:"key"`
	Description    string         `json:"description"`
	Available      bool           `json:"available"`
	Revision       int64          `json:"revision"`
	ArgumentSchema map[string]any `json:"argument_schema"`
	Plans          []ActionPlan   `json:"plans,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	Violations     []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"violations,omitempty"`
}

func (e EndingCapability) Label() string {
	if label, ok := e.Metadata["label"].(string); ok && label != "" {
		return label
	}
	return e.Description
}

func (e EndingCapability) Reason() string {
	if len(e.Violations) > 0 {
		return e.Violations[0].Message
	}
	return "当前条件不满足"
}
