package distributed

import (
	"time"

	"github.com/CS-SI/SafeScale/lib/utils/data"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
)

type Storage interface {
	// Write writes the value in key. If key already exists, will replace it
	Write(key string, value data.Serializable) fail.Error
	// WriteIfMissing writes only value in key if it does not yet exist. If exists, return *fail.ErrDuplicate
	WriteIfMissing(key string, value data.Serializable) fail.Error
	// Read reads the content of the key, and returned the value and the time of the last update
	Read(key string) (value data.Serializable, lastModified time.Time, xerr fail.Error)
	// KeyExists tells if the key already exists
	KeyExists(key string) (bool, fail.Error)
	// Delete removes the key from storage
	Delete(key string) fail.Error
}
