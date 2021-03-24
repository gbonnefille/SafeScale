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

package concurrency

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/CS-SI/SafeScale/lib/utils/data"
)

type SimpleMemory struct {
	content string
	painful int
}

func NewSimpleMemory(num int, cts string) *SimpleMemory {
	return &SimpleMemory{
		content: cts,
		painful: num,
	}
}

func (m SimpleMemory) Clone() data.Clonable {
	return NewSimpleMemory(0, "").Replace(&m)
}

func (m *SimpleMemory) Replace(clonable data.Clonable) data.Clonable {
	*m = *clonable.(*SimpleMemory)
	return m
}

func TestCloneable(t *testing.T) {
	a := NewSimpleMemory(9, "death")
	b := a.Clone().(*SimpleMemory)

	a.painful = 3

	ieq := reflect.DeepEqual(a, b)
	assert.False(t, ieq)

	a.painful = 9

	ieq = reflect.DeepEqual(a, b)
	assert.True(t, ieq)

	a.content = "despair"

	ieq = reflect.DeepEqual(a, b)
	assert.False(t, ieq)
}
