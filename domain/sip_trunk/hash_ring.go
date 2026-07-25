package sip_trunk

import (
	"hash/fnv"
	"sort"
)

func AssignOwner(key string, replicaIDs []string) string {
	if len(replicaIDs) == 0 {
		return ""
	}
	var bestID string
	var bestScore uint64
	first := true
	for _, rid := range replicaIDs {
		if rid == "" {
			continue
		}
		s := score(key, rid)
		if first || s > bestScore || (s == bestScore && rid < bestID) {
			bestScore = s
			bestID = rid
			first = false
		}
	}
	return bestID
}

func AssignAll(keys []string, replicaIDs []string) map[string]string {
	sorted := append([]string(nil), replicaIDs...)
	sort.Strings(sorted)
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		out[k] = AssignOwner(k, sorted)
	}
	return out
}

func score(key, replicaID string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(key))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(replicaID))
	x := h.Sum64()
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}
