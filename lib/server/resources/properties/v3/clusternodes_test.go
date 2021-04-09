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

package propertiesv3

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNodes_Clone(t *testing.T) {
	node := &ClusterNode{
		ID:        "",
		Name:      "Something",
		PublicIP:  "",
		PrivateIP: "",
	}

	ct := newClusterNodes()
	ct.ByNumericalID[1] = node
	clonedCt, ok := ct.Clone().(*ClusterNodes)
	if !ok {
		t.Fail()
	}

	assert.Equal(t, ct, clonedCt)
	clonedCt.ByNumericalID[1].Name = "Else"

	areEqual := reflect.DeepEqual(ct, clonedCt)
	if areEqual {
		t.Error("It's a shallow clone !")
		t.FailNow()
	}
}

func TestNodes_Clone2(t *testing.T) {
	node := &ClusterNode{
		ID:        "",
		Name:      "Something",
		PublicIP:  "",
		PrivateIP: "",
	}

	ct := newClusterNodes()
	ct.ByNumericalID[1] = node
	clonedCt, ok := ct.Clone().(*ClusterNodes)
	if !ok {
		t.Fail()
	}

	assert.Equal(t, ct, clonedCt)
	clonedCt.Masters = append(clonedCt.Masters, 10)

	areEqual := reflect.DeepEqual(ct, clonedCt)
	if areEqual {
		t.Error("It's a shallow clone !")
		t.FailNow()
	}
}
