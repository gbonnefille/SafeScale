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
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// Run with arguments: -convey-story

func TestNotEmpty(t *testing.T) {
	// Only pass t into top-level Convey calls
	Convey("Given no preconditions", t, func() {
		Convey("When a keepalive configuration is retrieved from system", func() {
			kat := newKeepAliveCfgFromSystem()
			Convey("its string repr is not empty", func() {
				So(kat.String(), ShouldNotBeEmpty)
			})
		})
	})

	Convey("Given no preconditions", t, func() {
		Convey("When default keepalive configuration is used", func() {
			kat := newDefaultKeepAliveCfg()
			Convey("its string repr is not empty", func() {
				So(kat.String(), ShouldNotBeEmpty)
			})

			Convey("its keepalive is of 7200 s", func() {
				So(kat.tcpKeepaliveTime, ShouldAlmostEqual, 7200)
			})
			Convey("its keepalive interval is 75 s", func() {
				So(kat.tcpKeepaliveIntvl, ShouldAlmostEqual, 75)
			})
			Convey("its keepalive number of probes is 9", func() {
				So(kat.tcpKeepaliveProbes, ShouldAlmostEqual, 9)
			})
		})
	})

}
