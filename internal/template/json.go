package template

// JSON types that match TypeScript validation tool expectations
type ValidationResult struct {
	DomainHash     string          `json:"domain_hash"`
	MessageHash    string          `json:"message_hash"`
	TargetSafe     string          `json:"target_safe"`
	StateOverrides []StateOverride `json:"state_overrides"`
	StateChanges   []StateChange   `json:"state_changes"`
}

type StateOverride struct {
	Name      string     `json:"name"`
	Address   string     `json:"address"`
	Overrides []Override `json:"overrides"`
}

type StateChange struct {
	Name    string   `json:"name"`
	Address string   `json:"address"`
	Changes []Change `json:"changes"`
}

type Override struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Description string `json:"description"`
}

type Change struct {
	Key         string `json:"key"`
	Before      string `json:"before"`
	After       string `json:"after"`
	Description string `json:"description"`
}
