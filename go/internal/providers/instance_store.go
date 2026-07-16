package providers

import (
	"slices"
	"sync"
)

type instanceStore interface {
	put(instance *Instance)
	get(instanceID string) (*Instance, bool)
	update(instanceID string, apply func(*Instance)) bool
	list() []*Instance
	listByProvider(providerType ProviderType) []*Instance
	listByWorkspace(workspaceID string) []*Instance
	findReusableStopped(providerType ProviderType, req *ProvisionRequest) *Instance
	findByWorker(workerID string) (*Instance, bool)
	findByProviderRef(providerType ProviderType, providerID string) (*Instance, bool)
}

type inMemoryInstanceStore struct {
	mu        sync.RWMutex
	instances map[string]*Instance
}

func newInMemoryInstanceStore() *inMemoryInstanceStore {
	return &inMemoryInstanceStore{
		instances: make(map[string]*Instance),
	}
}

func (s *inMemoryInstanceStore) put(instance *Instance) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[instance.ID] = cloneInstance(instance)
}

func (s *inMemoryInstanceStore) get(instanceID string) (*Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	instance, exists := s.instances[instanceID]
	if !exists {
		return nil, false
	}
	return cloneInstance(instance), true
}

func (s *inMemoryInstanceStore) update(instanceID string, apply func(*Instance)) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	instance, exists := s.instances[instanceID]
	if !exists {
		return false
	}
	apply(instance)
	return true
}

func (s *inMemoryInstanceStore) list() []*Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances := make([]*Instance, 0, len(s.instances))
	for _, instance := range s.instances {
		instances = append(instances, cloneInstance(instance))
	}
	return instances
}

func (s *inMemoryInstanceStore) listByProvider(providerType ProviderType) []*Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var instances []*Instance
	for _, instance := range s.instances {
		if instance.Provider == providerType {
			instances = append(instances, cloneInstance(instance))
		}
	}
	return instances
}

func (s *inMemoryInstanceStore) listByWorkspace(workspaceID string) []*Instance {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var instances []*Instance
	for _, instance := range s.instances {
		if instance.WorkspaceID == workspaceID {
			instances = append(instances, cloneInstance(instance))
		}
	}
	return instances
}

func (s *inMemoryInstanceStore) findReusableStopped(providerType ProviderType, req *ProvisionRequest) *Instance {
	if len(req.Models) == 0 {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	var best *Instance
	for _, inst := range s.instances {
		if inst.Provider != providerType || inst.WorkspaceID != req.WorkspaceID {
			continue
		}
		if inst.Status != InstanceStatusStopped {
			continue
		}
		if inst.GPUType != req.GPUType || inst.GPUCount != req.GPUCount {
			continue
		}
		if inst.Engine.OrDefault() != req.Engine.OrDefault() {
			continue
		}
		if !sameModels(inst.Models, req.Models) {
			continue
		}
		if best == nil || stoppedAtUnix(inst) > stoppedAtUnix(best) {
			best = inst
		}
	}

	return cloneInstance(best)
}

func (s *inMemoryInstanceStore) findByWorker(workerID string) (*Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, instance := range s.instances {
		if instance.WorkerID == workerID {
			return cloneInstance(instance), true
		}
	}
	return nil, false
}

func (s *inMemoryInstanceStore) findByProviderRef(providerType ProviderType, providerID string) (*Instance, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, instance := range s.instances {
		if instance.Provider == providerType && instance.ProviderID == providerID {
			return cloneInstance(instance), true
		}
	}
	return nil, false
}

func sameModels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	slices.Sort(left)
	slices.Sort(right)
	return slices.Equal(left, right)
}

func stoppedAtUnix(inst *Instance) int64 {
	if inst.StoppedAt != nil {
		return inst.StoppedAt.Unix()
	}
	return inst.CreatedAt.Unix()
}
