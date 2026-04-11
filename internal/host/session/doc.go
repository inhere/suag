// Package session provides the host-side session lifecycle foundation for Axon.
//
// It owns product-layer session/turn/run state and keeps that boundary separate
// from pkg/agentcore's single-agent runtime kernel.
package session
