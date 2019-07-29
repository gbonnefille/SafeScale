/*
 * Copyright 2018-2019, CS Systemes d'Information, http://www.c-s.fr
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

package providers

// Capabilities represents key/value configuration.
type Capabilities struct {
	// PublicVirtualIp indicates if the provider has the capability to provide a Virtual IP with public IP address
	PublicVirtualIp bool
	// PrivateVirtualIp indicates if the provider has the capability to provide a Virtual IP with private IP address
	PrivateVirtualIp bool
	// Layer3Networking indicates if the provider uses Layer3 networking
	Layer3Networking bool
}
