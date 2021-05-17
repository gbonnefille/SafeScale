package distributed

import (
	"sync"
	"time"

	"github.com/CS-SI/SafeScale/lib/utils/concurrency"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
	uuid "github.com/satori/go.uuid"
)

type Participant interface {
	NewLock(key string) (Lock, fail.Error)
}

// ParticipantIdentity allows to store identity information of participant (being local or remote)
type ParticipantIdentity struct {
	ID string
	// FIXME: identify needed fields to add and implement
}

// participant is implementation of Participant interface
type participant struct {
	storagePath       string
	storage           Storage
	heartbeatTask     concurrency.Task
	identity          ParticipantIdentity
	heartbeatInterval time.Duration
	mu                sync.Mutex
	locks             map[string]Lock
}

const minimumHeartbeatInterval = time.Second

// NewParticipant creates a new participant and ensure heartbeat on storage
func NewParticipant(storage Storage, heartbeatInterval time.Duration) (*participant, fail.Error) {
	if storage == nil {
		return nil, fail.InvalidParameterCannotBeNilError("storage")
	}
	if heartbeatInterval < minimumHeartbeatInterval {
		heartbeatInterval = minimumHeartbeatInterval
	}

	id, err := uuid.NewV4()
	if err != nil {
		return nil, fail.ConvertError(err)
	}

	// With a new Participant is started a go routine than has to refresh the participant on the storage,
	// allowing other participants to know the participant is still valid
	heartbeatTask, xerr := concurrency.NewTask()
	if xerr != nil {
		return nil, xerr
	}

	participantInstance := &participant{
		identity: ParticipantIdentity{
			ID: id.String(),
		},
		storage:           storage,
		heartbeatInterval: heartbeatInterval,
		heartbeatTask:     heartbeatTask,
		locks:             make(map[string]Lock),
	}
	_, xerr = participantInstance.heartbeatTask.Start(participantInstance.taskHeartbeat, nil)
	if xerr != nil {
		return nil, xerr
	}

	return participantInstance, nil
}

// taskHeartbeat refreshes the content of the participant on the storage
func (s *participant) taskHeartbeat(task concurrency.Task, _ concurrency.TaskParameters) (concurrency.TaskResult, fail.Error) {
	fullPath := s.storagePath + s.identity.ID

	for {
		if task.Aborted() {
			break
		}

		// Update participant file in storage
		xerr := s.storage.Write(fullPath, s)
		if xerr != nil {
			return nil, xerr
		}

		// Wait for s.heartbeatInterval for next update
		// FIXME: we need a hard heartbeat, time.Sleep() is not a good way to do that
		time.Sleep(s.heartbeatInterval)
	}

	// FIXME: Frees all locks registered with participant
	// FIXME: removes participant file from storage
	xerr := s.storage.Delete(fullPath)
	if xerr != nil {
		return nil, xerr
	}

	return nil, nil
}

// isNullValue tells if participant is not correctly initialized (true if not, false if yes)
func (s participant) isNullValue() bool {
	return s.identity.ID != "" && s.storage != nil
}

// Serialize serializes the content of s into a slice of bytes
func (s *participant) Serialize() ([]byte, error) {
	return nil, fail.NotImplementedError()
}

// Deserialize fills instance from slice of bytes
func (s *participant) Deserialize(data []byte) error {
	return fail.NotImplementedError()
}

func (s participant) Identity() ParticipantIdentity {
	return s.identity
}

// NewLock creates a new distributed lock from participant
func (s participant) NewLock(key string) (Lock, fail.Error) {
	l, xerr := newLock(&s, key)
	if xerr != nil {
		return nil, xerr
	}

	xerr = s.pushLock(key, l)
	if xerr != nil {
		return nil, xerr
	}

	return l, nil
}

func (s *participant) pushLock(key string, lock Lock) fail.Error {
	if s == nil {
		return fail.InvalidInstanceError()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.locks[key]; ok {
		return fail.DuplicateError("there is already a lock for key '%s'", key)
	}

	s.locks[key] = lock
	return nil
}

func (s *participant) popLock(key string) fail.Error {
	if s == nil {
		return fail.InvalidInstanceError()
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if lock, ok := s.locks[key]; ok {
		xerr := lock.Unlock()
		if xerr != nil {
		}
		return xerr
		delete(s.locks, key)
	}
	return nil
}

// Close ends the distributed participant, releasing all locks
func (s participant) Close() fail.Error {
	return s.heartbeatTask.Abort()
}

// loadRemoteParticipant will read participant file on storage and format it in RemoteParticipant
func (s participant) loadRemoteParticipant(id string) (ParticipantIdentity, fail.Error) {
	// FIXME: implement
	return ParticipantIdentity{}, fail.NotImplementedError()
}