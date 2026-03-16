package agent

import (
	"testing"
)

func TestParseSoulBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		check   func(*testing.T, *Soul)
	}{
		{
			name: "valid front matter",
			input: `---
name: "Architect"
role: "System designer"
expertise:
  - "Go"
  - "APIs"
tools:
  - "file_read"
constraints:
  - "Only design, no code"
---
`,
			check: func(t *testing.T, s *Soul) {
				if s.Name != "Architect" {
					t.Errorf("Name = %q, want Architect", s.Name)
				}
				if s.Role != "System designer" {
					t.Errorf("Role = %q, want System designer", s.Role)
				}
				if len(s.Expertise) != 2 {
					t.Errorf("Expertise len = %d, want 2", len(s.Expertise))
				}
				if s.Persona == "" {
					t.Error("Persona should be auto-built when empty")
				}
			},
		},
		{
			name: "explicit persona",
			input: `---
name: "Bot"
role: "Helper"
persona: "You are a helpful bot."
---
`,
			check: func(t *testing.T, s *Soul) {
				if s.Persona != "You are a helpful bot." {
					t.Errorf("Persona = %q", s.Persona)
				}
			},
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: false, // empty YAML is valid (zero value)
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			soul, err := ParseSoulBytes([]byte(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseSoulBytes() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.check != nil && soul != nil {
				tc.check(t, soul)
			}
		})
	}
}

func TestStateMachine(t *testing.T) {
	tests := []struct {
		name        string
		from        AgentState
		to          AgentState
		wantErr     bool
	}{
		{"pending→running ok", AgentPending, AgentRunning, false},
		{"running→completed ok", AgentRunning, AgentCompleted, false},
		{"running→suspended ok", AgentRunning, AgentSuspended, false},
		{"suspended→running ok", AgentSuspended, AgentRunning, false},
		{"pending→completed invalid", AgentPending, AgentCompleted, true},
		{"completed→running invalid", AgentCompleted, AgentRunning, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sm := newStateMachine(tc.from)
			err := sm.Transition(tc.to)
			if (err != nil) != tc.wantErr {
				t.Errorf("Transition(%s→%s) err=%v wantErr=%v", tc.from, tc.to, err, tc.wantErr)
			}
		})
	}
}
