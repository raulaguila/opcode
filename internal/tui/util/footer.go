package util

// FooterState is presentation data, not authority to change the task's mode.
type FooterState struct {
	ContextTokens, ContextWindow   int64
	ContextKnown, ContextEstimated bool
	TokensPerSecond                float64
	SpeedEstimated                 bool
	Mode, Activity                 string
	Busy, Terminal                 bool
	Pending                        int
}
