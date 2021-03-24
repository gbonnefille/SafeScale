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

package errcontrol

import "testing"

func TestWithContext(t *testing.T) {
	err := CrashSetup("crash_test_helper.go:9:1") // Line 9 of crash_test_helper will fail with a probability of 100% (1)
	if err != nil {
		t.FailNow()
	}

	broken := checkYeah()
	if broken == nil {
		t.FailNow()
	}

	unbroken := checkYeahUntouched()
	if unbroken != nil {
		t.FailNow()
	}
}

func TestWithEmptyContext(t *testing.T) {
	err := CrashSetup("")
	if err != nil {
		t.FailNow()
	}

	broken := checkYeah()
	if broken != nil {
		t.Errorf(broken.Error())
		t.FailNow()
	}

	unbroken := checkYeahUntouched()
	if unbroken != nil {
		t.Errorf(unbroken.Error())
		t.FailNow()
	}
}
