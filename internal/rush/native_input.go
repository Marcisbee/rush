package rush

type NativeInputRequest struct {
	Action string  `json:"action"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	Text   string  `json:"text,omitempty"`
	Key    string  `json:"key,omitempty"`
}

type nativeInput interface {
	Do(NativeInputRequest) error
	Close() error
}
