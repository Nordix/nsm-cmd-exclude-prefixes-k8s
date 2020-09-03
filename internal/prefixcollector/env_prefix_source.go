// Copyright (c) 2020 Doc.ai and/or its affiliates.
//
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at:
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package prefixcollector

import (
	"net"

	"github.com/sirupsen/logrus"
)

// EnvPrefixSource is environment excluded prefixes source
type EnvPrefixSource struct {
	prefixes []string
}

// Prefixes returns prefixes from source
func (e *EnvPrefixSource) Prefixes() []string {
	return e.prefixes
}

// NewEnvPrefixSource creates EnvPrefixSource
func NewEnvPrefixSource(uncheckedPrefixes []string) *EnvPrefixSource {
	prefixes := getValidatedPrefixes(uncheckedPrefixes)
	source := &EnvPrefixSource{
		prefixes: prefixes,
	}
	return source
}

// getValidatedPrefixes returns list of validated via CIDR notation parsing prefixes
func getValidatedPrefixes(prefixes []string) []string {
	var validatedPrefixes []string
	for _, prefix := range prefixes {
		_, _, err := net.ParseCIDR(prefix)
		if err != nil {
			logrus.Errorf("Error parsing CIDR from %v: %v", prefix, err)
			continue
		}
		validatedPrefixes = append(validatedPrefixes, prefix)
	}

	return validatedPrefixes
}
