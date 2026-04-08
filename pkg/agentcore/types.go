package agentcore

import "github.com/inhere/axon/pkg/agentcore/api"

type RunInput = api.RunInput
type RunStatus = api.RunStatus

const (
	RunIdle      = api.RunIdle
	RunRunning   = api.RunRunning
	RunYielded   = api.RunYielded
	RunCompleted = api.RunCompleted
	RunFailed    = api.RunFailed
)

type ResumeMode = api.ResumeMode

const (
	ResumeAuto      = api.ResumeAuto
	ResumeFromState = api.ResumeFromState
	ResumeWithInput = api.ResumeWithInput
)

type Message = api.Message
type Artifact = api.Artifact
type Usage = api.Usage
type Yield = api.Yield
type RunState = api.RunState
type RunResult = api.RunResult
