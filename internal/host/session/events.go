package session

const (
	EventRunQueued   = "run.queued"
	EventRunStarted  = "run.started"
	EventRunStatus   = "run.status"
	EventRunFinished = "run.finished"
	EventRunFailed   = "run.failed"
	EventRunCanceled = "run.canceled"

	EventRunExternalActivity     = "run.external.activity"
	EventRunExternalMessageDelta = "run.external.message.delta"
	EventRunExternalMessageEnd   = "run.external.message.end"
)
