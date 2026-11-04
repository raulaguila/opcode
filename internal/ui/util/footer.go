package util

// FooterState is presentation data, not authority to change the task's mode.
type FooterState struct {
	ContextTokens, ContextWindow   int64
	ContextKnown, ContextEstimated bool
	TokensPerSecond                float64
	SpeedEstimated                 bool
	SpeedLive, AwaitingModel       bool
	Mode, Activity                 string
	ApprovalPolicy                 string
	Busy, Terminal                 bool
	WaitingForAnswer               bool
	ElapsedSeconds                 int
	ElapsedKnown                   bool
	Pending                        int
}
