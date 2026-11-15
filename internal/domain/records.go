package domain

import "time"

type JournalEntry struct {
	Cycle   string    `json:"cycle"`
	TaskID  string    `json:"taskId"`
	Title   string    `json:"title"`
	Note    string    `json:"note,omitempty"`
	Minutes int       `json:"minutes,omitempty"`
	At      time.Time `json:"at"`
}

type DisableMode string

const (
	DisableUntilTomorrow DisableMode = "tomorrow"
	DisableQuit          DisableMode = "quit"
	DisableNight         DisableMode = "night"
)

type DisableEvent struct {
	Cycle  string      `json:"cycle"`
	Mode   DisableMode `json:"mode"`
	Reason string      `json:"reason,omitempty"`
	At     time.Time   `json:"at"`
}
