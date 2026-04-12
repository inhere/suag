package session

const (
	EventRunQueued   = "run.queued"
	EventRunStarted  = "run.started"
	EventRunFinished = "run.finished"
	EventRunFailed   = "run.failed"

	EventRunExternalActivity     = "run.external.activity"
	EventRunExternalMessageDelta = "run.external.message.delta"
	EventRunExternalMessageEnd   = "run.external.message.end"
)
