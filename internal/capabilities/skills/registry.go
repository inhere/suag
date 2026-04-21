package skills

import "strings"

// Registry stores capability-skill descriptors discovered from local or user
// provided skill sources.
type Registry struct {
	descriptors []Descriptor
}

func NewRegistry() *Registry {
	return &Registry{}
}

func (r *Registry) Register(desc Descriptor) {
	if strings.TrimSpace(desc.Name) == "" {
		return
	}
	r.descriptors = append(r.descriptors, cloneDescriptor(desc))
}

func (r *Registry) List() []Descriptor {
	if r == nil || len(r.descriptors) == 0 {
		return nil
	}

	out := make([]Descriptor, 0, len(r.descriptors))
	for _, desc := range r.descriptors {
		out = append(out, cloneDescriptor(desc))
	}
	return out
}

func cloneDescriptor(desc Descriptor) Descriptor {
	return Descriptor{
		Name:              strings.TrimSpace(desc.Name),
		Description:       strings.TrimSpace(desc.Description),
		SourcePath:        strings.TrimSpace(desc.SourcePath),
		TriggerHints:      cloneStrings(desc.TriggerHints),
		ToolAllowlist:     cloneStrings(desc.ToolAllowlist),
		SystemPromptParts: cloneStrings(desc.SystemPromptParts),
		ResourceHints:     cloneStrings(desc.ResourceHints),
	}
}

func cloneStrings(src []string) []string {
	if len(src) == 0 {
		return nil
	}

	out := make([]string, 0, len(src))
	for _, item := range src {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		out = append(out, item)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
