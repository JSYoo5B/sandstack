package compute

import "time"

const (
	ActionCreate       = "create"
	ActionStop         = "stop"
	ActionStart        = "start"
	ActionReboot       = "reboot"
	ActionPause        = "pause"
	ActionUnpause      = "unpause"
	ActionSuspend      = "suspend"
	ActionResume       = "resume"
	ActionLock         = "lock"
	ActionUnlock       = "unlock"
	ActionAttachVolume = "attach_volume"
	ActionDetachVolume = "detach_volume"
	ActionCreateImage  = "createImage"
)

// Action is a server action record keyed by the request that started it.
type Action struct {
	Name      string
	ServerID  string
	RequestID string
	ProjectID string
	UserID    string
	StartTime time.Time
}
