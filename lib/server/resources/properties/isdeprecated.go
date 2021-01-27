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

package properties

type Depreciation interface {
	IsDeprecated() bool
	DeprecatedBy() string
	NewProperty() string
}

// depreciation tells if a property is deprecated and replaced by a newer version; contains the name of the new property and the release that did the upgrade
type depreciation struct {
	Deprecated bool   `json:"deprecated,omitempty"`
	ByRelease  string `json:"by_release,omitempty"`
	Property   string `json:"new_property,omitempty"`
}

func NewDepreciation(replacer, version string) Depreciation {
	return &depreciation{
		Deprecated: true,
		ByRelease: version,
		Property: replacer,
	}
}

func (d depreciation) IsDeprecated() bool {
	return d.Deprecated
}

func (d depreciation) DeprecatedBy() string {
	return d.ByRelease
}

func (d depreciation) NewProperty() string {
	return d.Property
}
