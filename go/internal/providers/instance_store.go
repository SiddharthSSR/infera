package providers

import (
	"crypto/sha256"
	"slices"
	"sync"
	"time"
)

type instanceStore interface {
	put(instance *Instance) error
	get(instanceID string) (*Instance, bool, error)
	update(instanceID string, apply func(*Instance)) (bool, error)
	updateLifecycle(instanceID string, apply func(*Instance)) (bool, error)
	updateIfLifecycleVersion(instanceID string, expectedVersion int64, apply func(*Instance)) (bool, error)
	list() ([]*Instance, error)
	listByProvider(providerType ProviderType) ([]*Instance, error)
	listByWorkspace(workspaceID string) ([]*Instance, error)
	findReusableStopped(providerType ProviderType, req *ProvisionRequest) (*Instance, error)
	findByWorker(workerID string) (*Instance, bool, error)
	findByProviderRef(providerType ProviderType, providerID string) (*Instance, bool, error)
}

type credentialInstanceStore interface {
	authenticateWorkerTokenHash(hash [sha256.Size]byte) (*Instance, bool, error)
	authorizeWorkerBinding(instanceID, workerID string) error
	linkWorker(instanceID, workerID string, now time.Time) error
	workerCredentialForWorker(workerID string) (string, bool, error)
	Close() error
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

func (s *inMemoryInstanceStore) put(instance *Instance) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := cloneInstance(instance)
	if stored.LifecycleVersion <= 0 {
		stored.LifecycleVersion = 1
	}
	s.instances[instance.ID] = stored
	return nil
}

func (s *inMemoryInstanceStore) get(instanceID string) (*Instance, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	instance, exists := s.instances[instanceID]
	if !exists {
		return nil, false, nil
	}
	return cloneInstance(instance), true, nil
}

func (s *inMemoryInstanceStore) update(instanceID string, apply func(*Instance)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instance, exists := s.instances[instanceID]
	if !exists {
		return false, nil
	}
	apply(instance)
	return true, nil
}

func (s *inMemoryInstanceStore) updateLifecycle(instanceID string, apply func(*Instance)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instance, exists := s.instances[instanceID]
	if !exists {
		return false, nil
	}
	apply(instance)
	instance.LifecycleVersion++
	return true, nil
}

func (s *inMemoryInstanceStore) updateIfLifecycleVersion(instanceID string, expectedVersion int64, apply func(*Instance)) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	instance, exists := s.instances[instanceID]
	if !exists || instance.LifecycleVersion != expectedVersion {
		return false, nil
	}
	apply(instance)
	instance.LifecycleVersion++
	return true, nil
}

func (s *inMemoryInstanceStore) list() ([]*Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	instances := make([]*Instance, 0, len(s.instances))
	for _, instance := range s.instances {
		instances = append(instances, cloneInstance(instance))
	}
	return instances, nil
}

func (s *inMemoryInstanceStore) listByProvider(providerType ProviderType) ([]*Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var instances []*Instance
	for _, instance := range s.instances {
		if instance.Provider == providerType {
			instances = append(instances, cloneInstance(instance))
		}
	}
	return instances, nil
}

func (s *inMemoryInstanceStore) listByWorkspace(workspaceID string) ([]*Instance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var instances []*Instance
	for _, instance := range s.instances {
		if instance.WorkspaceID == workspaceID {
			instances = append(instances, cloneInstance(instance))
		}
	}
	return instances, nil
}

func (s *inMemoryInstanceStore) findReusableStopped(providerType ProviderType, req *ProvisionRequest) (*Instance, error) {
	if len(req.Models) == 0 {
		return nil, nil
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

	return cloneInstance(best), nil
}

func (s *inMemoryInstanceStore) findByWorker(workerID string) (*Instance, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, instance := range s.instances {
		if instance.WorkerID == workerID {
			return cloneInstance(instance), true, nil
		}
	}
	return nil, false, nil
}

func (s *inMemoryInstanceStore) findByProviderRef(providerType ProviderType, providerID string) (*Instance, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, instance := range s.instances {
		if instance.Provider == providerType && instance.ProviderID == providerID {
			return cloneInstance(instance), true, nil
		}
	}
	return nil, false, nil
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
