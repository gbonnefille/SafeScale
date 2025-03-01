/*
 * Copyright 2018-2021, CS Systemes d'Information, http://csgroup.eu
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package serialize

import (
	"encoding/json"
	"sync"

	"github.com/sirupsen/logrus"

	"github.com/CS-SI/SafeScale/lib/utils/concurrency"
	"github.com/CS-SI/SafeScale/lib/utils/data"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
)

// jsonProperty contains data and a RWMutex to handle sync
type jsonProperty struct {
	*concurrency.Shielded
	module, key string
}

func (jp jsonProperty) Clone() data.Clonable {
	newP := &jsonProperty{}
	return newP.Replace(&jp)
}

func (jp *jsonProperty) Replace(clonable data.Clonable) data.Clonable {
	// Do not test with isNull(), it's allowed to clone a null value...
	if jp == nil || clonable == nil {
		return jp
	}

	srcP := clonable.(*jsonProperty)
	*jp = *srcP
	jp.Shielded = srcP.Shielded.Clone()
	return jp
}

// JSONProperties ...
type JSONProperties struct {
	// properties jsonProperties
	Properties data.Map
	// This lock is used to make sure addition or removal of keys in JSonProperties won't collide in go routines
	sync.RWMutex
	module string
}

// NewJSONProperties creates a new JSonProperties instance
func NewJSONProperties(module string) (*JSONProperties, fail.Error) {
	if module == "" {
		return nil, fail.InvalidParameterCannotBeEmptyStringError("module")
	}
	return &JSONProperties{
		Properties: data.Map{},
		module:     module,
	}, nil
}

// Lookup tells if a key is present in JSonProperties
func (x *JSONProperties) Lookup(key string) bool {
	if x == nil {
		return false
	}

	x.RLock()
	defer x.RUnlock()

	_, ok := x.Properties[key]
	return ok
}

// Clone ...
func (x *JSONProperties) Clone() *JSONProperties {
	if x == nil {
		return nil
	}

	x.RLock()
	defer x.RUnlock()

	newP := &JSONProperties{
		module: x.module,
	}
	for k, v := range x.Properties {
		newP.Properties[k] = v
	}
	return newP
}

// Count returns the number of properties available
func (x *JSONProperties) Count() uint {
	if x == nil {
		return 0
	}
	return uint(len(x.Properties))
}

// Inspect allows to consult the content of the property 'key' inside 'inspector' function
// Changes in the property won't be kept
func (x *JSONProperties) Inspect(key string, inspector func(clonable data.Clonable) fail.Error) fail.Error {
	if x == nil {
		return fail.InvalidInstanceError()
	}
	if x.Properties == nil {
		return fail.InvalidInstanceContentError("x.properties", "can't be nil")
	}
	if x.module == "" {
		return fail.InvalidInstanceContentError("x.module", "can't be empty string")
	}
	if key == "" {
		return fail.InvalidParameterCannotBeEmptyStringError("key")
	}
	if inspector == nil {
		return fail.InvalidParameterCannotBeNilError("inspector")
	}

	var (
		item  *jsonProperty
		found bool
	)
	x.RLock()
	if item, found = x.Properties[key].(*jsonProperty); !found {
		zeroValue := PropertyTypeRegistry.ZeroValue(x.module, key)
		item = &jsonProperty{
			Shielded: concurrency.NewShielded(zeroValue),
			module:   x.module,
			key:      key,
		}
		x.Properties[key] = item
	}
	clone := item.Clone()
	x.RUnlock()

	return clone.(*jsonProperty).Shielded.Inspect(inspector)
}

// Alter is used to lock an extension for write
// Returns a pointer to LockedEncodedExtension, on which can be applied method 'Use()'
// If no extension exists corresponding to the key, an empty one is created (in other words, this call
// can't fail because a key doesn't exist).
// 'alterer' can use a special error to tell the outside there was no change : fail.ErrAlteredNothing, which can be
// generated with fail.AlteredNothingError().
func (x *JSONProperties) Alter(key string, alterer func(data.Clonable) fail.Error) fail.Error {
	if x == nil {
		return fail.InvalidInstanceError()
	}
	if x.Properties == nil {
		return fail.InvalidInstanceContentError("x.properties", "cannot be nil")
	}
	if x.module == "" {
		return fail.InvalidInstanceContentError("x.module", "cannot be empty string")
	}
	if key == "" {
		return fail.InvalidParameterCannotBeEmptyStringError("key")
	}
	if alterer == nil {
		return fail.InvalidParameterCannotBeNilError("alterer")
	}

	x.Lock()
	defer x.Unlock()

	var (
		item  *jsonProperty
		found bool
	)

	if item, found = x.Properties[key].(*jsonProperty); !found {
		zeroValue := PropertyTypeRegistry.ZeroValue(x.module, key)
		item = &jsonProperty{
			Shielded: concurrency.NewShielded(zeroValue),
			module:   x.module,
			key:      key,
		}
		x.Properties[key] = item
	}

	clone := item.Clone()
	if xerr := clone.(*jsonProperty).Alter(alterer); xerr != nil {
		return xerr
	}

	_ = item.Replace(clone)
	return nil
}

// SetModule allows to change the module of the JSONProperties (used to "contextualize" Property Types)
func (x *JSONProperties) SetModule(module string) fail.Error {
	if x == nil {
		return fail.InvalidInstanceError()
	}
	if module == "" {
		return fail.InvalidParameterError("key", "can't be empty string")
	}

	x.Lock()
	defer x.Unlock()

	if x.module == "" {
		x.module = module
	}
	return nil
}

// Serialize ...
// satisfies interface data.Serializable
func (x *JSONProperties) Serialize() ([]byte, fail.Error) {
	if x == nil {
		return nil, fail.InvalidInstanceError()
	}
	if x.Properties == nil {
		return nil, fail.InvalidParameterError("x.properties", "can't be nil")
	}

	x.RLock()
	defer x.RUnlock()

	var mapped = map[string]string{}
	for k, v := range x.Properties {
		ser, err := v.(*jsonProperty).Serialize()
		if err != nil {
			return nil, err
		}
		mapped[k] = string(ser)
	}
	r, jserr := json.Marshal(mapped)
	if jserr != nil {
		return nil, fail.NewError(jserr.Error())
	}
	return r, nil
}

// Deserialize ...
// Returns fail.SyntaxError if an JSON syntax error happens
// satisfies interface data.Serializable
func (x *JSONProperties) Deserialize( /*task concurrency.Task, */ buf []byte) (xerr fail.Error) {
	if x == nil {
		return fail.InvalidInstanceError()
	}

	defer fail.OnPanic(&xerr) // json.Unmarshal may panic

	x.Lock()
	defer x.Unlock()

	// Decode JSON data
	var unjsoned = map[string]string{}
	if jserr := json.Unmarshal(buf, &unjsoned); jserr != nil {
		switch jserr.(type) {
		case *json.SyntaxError:
			return fail.SyntaxError(jserr.Error())
		default:
			logrus.Tracef("*JSONProperties.Deserialize(): Unmarshalling buf to string failed: %s", jserr.Error())
			return fail.NewError(jserr.Error())
		}
	}

	var (
		prop *jsonProperty
		ok   bool
	)
	for k, v := range unjsoned {
		if prop, ok = x.Properties[k].(*jsonProperty); !ok {
			zeroValue := PropertyTypeRegistry.ZeroValue(x.module, k)
			item := &jsonProperty{
				Shielded: concurrency.NewShielded(zeroValue),
				module:   x.module,
				key:      k,
			}
			x.Properties[k] = item
			prop = item
		}
		err := prop.Shielded.Deserialize([]byte(v))
		if err != nil {
			return err
		}
	}
	return nil
}
