package cluster

import (
	"sort"
)

type store interface {
	SAdd(key string, members ...string) error
	SRem(key string, members ...string) error
	SMembers(key string) ([]string, error)
	Exists(key string) (bool, error)
}

const ClusterReplicasKey = "cluster:replicas"

const HeartbeatKeyPrefix = "replica:heartbeat:"

type Registry struct {
	shared store
}

func NewRegistry(shared store) *Registry {
	return &Registry{shared: shared}
}

func (r *Registry) Announce(replicaID string) error {
	if replicaID == "" {
		return nil
	}
	return r.shared.SAdd(ClusterReplicasKey, replicaID)
}

func (r *Registry) Withdraw(replicaID string) error {
	if replicaID == "" {
		return nil
	}
	return r.shared.SRem(ClusterReplicasKey, replicaID)
}

func (r *Registry) LiveReplicas() ([]string, error) {
	members, err := r.shared.SMembers(ClusterReplicasKey)
	if err != nil {
		return nil, err
	}
	live := make([]string, 0, len(members))
	for _, rid := range members {
		alive, err := r.shared.Exists(HeartbeatKeyPrefix + rid)
		if err != nil {

			live = append(live, rid)
			continue
		}
		if alive {
			live = append(live, rid)
			continue
		}

		_ = r.shared.SRem(ClusterReplicasKey, rid)
	}
	sort.Strings(live)
	return live, nil
}
