package customfield

type ViewerDefinition struct {
	Definition
	Readable bool `json:"readable"`
}

func ProjectFor(defs []*Definition, viewer Viewer) []ViewerDefinition {
	out := make([]ViewerDefinition, 0, len(defs))
	for _, def := range defs {
		if def == nil {
			continue
		}
		out = append(out, ViewerDefinition{Definition: *def, Readable: VisibleTo(def, viewer)})
	}
	return out
}
