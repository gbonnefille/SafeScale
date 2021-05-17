package distributed

import (
	"time"

	"github.com/CS-SI/SafeScale/lib/utils/fail"
)

type Lock interface {
	Lock(ttl time.Duration) fail.Error
	TryLock(ttl time.Duration) fail.Error
	TryLockFor(ttl time.Duration, timeout time.Duration) fail.Error
	Unlock() fail.Error
	IsLocked() (bool, string, fail.Error)
}

type lock struct {
	participant *participant
	key         string
	ttl         time.Duration
}

// newLock creates a new distributed lock
func newLock(par Participant, key string) (Lock, fail.Error) {
	if par == nil {
		return nil, fail.InvalidParameterCannotBeNilError("par")
	}
	if key == "" {
		return nil, fail.InvalidParameterCannotBeEmptyStringError("key")
	}
	participantInstance := par.(*participant)
	if participantInstance.isNullValue() {
		return nil, fail.InvalidParameterError("participantInstance", "cannot be a null value of 'participant'")
	}

	newLock := &lock{
		participant: participantInstance,
		key:         key,
	}
	return newLock, nil
}

func (l lock) Lock(ttl time.Duration) fail.Error {
	// // Store value of key in storage
	// xerr := l.participant.storage.Write(l.key, value)
	// if xerr != nil {
	// 	return xerr
	// }

	return fail.NotImplementedError()
}

func (l lock) TryLock(ttl time.Duration) fail.Error {
	_, _, xerr := l.participant.storage.Read(l.key)
	if xerr != nil {
		return xerr
	}

	return nil
}

func (l lock) TryLockFor(ttl time.Duration, timeout time.Duration) fail.Error {
	// // Store value of key in storage
	// xerr := l.participant.storage.Write(l.key, value)
	// if xerr != nil {
	// 	return xerr
	// }

	return fail.NotImplementedError()
}

// Unlock removed a distributed lock on a key
func (l lock) Unlock() fail.Error {
	// delete value of key from storage
	xerr := l.participant.storage.Delete(l.key)
	if xerr != nil {
		return xerr
	}

	return l.participant.popLock(l.key)
}

// IsLocked can be used to tell if the lock is acquired and by what participant
func (l lock) IsLocked() (bool, string, fail.Error) {
	// Gets participant used to manage Map
	sess := l.participant

	// Store value of key in storage
	_/*value*/, _/*lastUpdated*/, xerr := sess.storage.Read(l.key)
	if xerr != nil {
		return false, "", xerr
	}

	// FIXME: implement lock check
	return false, "", fail.NotImplementedError()
}

type LockState struct {
	Locked bool
	Participant struct {
		ID string
	}
}

// State returns information about the lock
func (l lock) State() (LockState, fail.Error) {
	locked, participantID, xerr := l.IsLocked()
	if xerr != nil {
		return LockState{}, xerr
	}
	if locked {
		out := LockState{
			Locked: true,
		}
		remoteParticipant, xerr := l.participant.loadRemoteParticipant(participantID)
		if xerr != nil {
			return out, xerr
		}

		out.Participant = remoteParticipant
		return out, nil
	}

	out := LockState{
		Locked: false,
	}
	return out, nil
}
