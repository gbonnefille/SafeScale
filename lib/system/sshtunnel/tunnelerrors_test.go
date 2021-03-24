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

package sshtunnel

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func LastUnwrap(in error) (err error) {
	if in == nil {
		return nil
	}

	last := in
	for {
		err = last
		u, ok := last.(interface {
			Unwrap() error
		})
		if !ok {
			break
		}
		last = u.Unwrap()
	}

	return err
}

func Test_tunnelError_Error(t *testing.T) {
	e := tunnelError{
		error:       fmt.Errorf("is that error happened: %w", fmt.Errorf("it seems the end is near")),
		isTimeout:   false,
		isTemporary: false,
	}

	require.NotNil(t, errors.Unwrap(e))

	depth1 := fmt.Errorf("Telling now: %w", e)
	depth2 := fmt.Errorf("What am i: %w", depth1)
	depth3 := fmt.Errorf("Just in case: %w", depth2)

	var captured tunnelError
	contains := errors.As(depth3, &captured)
	require.True(t, contains)

	ricochet := errors.Unwrap(depth3)
	require.True(t, strings.Contains(ricochet.Error(), "end is near") && strings.Contains(ricochet.Error(), "error happened") && strings.Contains(ricochet.Error(), "What am i") && strings.Contains(ricochet.Error(), "Telling now") && !strings.Contains(ricochet.Error(), "Just in case"))

	delorean := LastUnwrap(depth3)
	require.True(t, strings.Contains(delorean.Error(), "end is near"))
}
