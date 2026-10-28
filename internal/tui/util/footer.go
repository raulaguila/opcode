package util

// FooterState is presentation data, not authority to change the task's mode.
type FooterState struct {
	Mode, Activity string
	Busy, Terminal bool
	Pending        int
}
