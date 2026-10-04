package advertising

import "strings"

const metaMediaPrefix = "meta:"

func MetaMediaRef(kind MediaKind, metaID string) MediaRef {
	return MediaRef{Kind: kind, MediaID: metaMediaPrefix + metaID}
}

func (m MediaRef) MetaID() (string, bool) {
	return strings.CutPrefix(m.MediaID, metaMediaPrefix)
}

func (c CreativeDraft) MetaMediaRefs() []MediaRef {
	var refs []MediaRef
	for _, ref := range c.MediaRefs() {
		if _, ok := ref.MetaID(); ok {
			refs = append(refs, ref)
		}
	}
	return refs
}
