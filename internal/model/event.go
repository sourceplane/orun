package model

// EventContext is a provider-neutral representation of a CI event.
type EventContext struct {
	Provider   string `json:"provider"`
	Event      string `json:"event"`
	Action     string `json:"action,omitempty"`
	Ref        string `json:"ref,omitempty"`
	BaseRef    string `json:"baseRef,omitempty"`
	BaseSha    string `json:"baseSha,omitempty"`
	HeadSha    string `json:"headSha,omitempty"`
	Branch     string `json:"branch,omitempty"`
	IsFork     bool   `json:"isFork,omitempty"`
	Actor      string `json:"actor,omitempty"`
	Repository string `json:"repository,omitempty"`
}
