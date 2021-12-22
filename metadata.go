/*
 *  Copyright 2021 qitoi
 *
 *  Licensed under the Apache License, Version 2.0 (the "License");
 *  you may not use this file except in compliance with the License.
 *  You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 *  Unless required by applicable law or agreed to in writing, software
 *  distributed under the License is distributed on an "AS IS" BASIS,
 *  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 *  See the License for the specific language governing permissions and
 *  limitations under the License.
 */

package spacedl

import (
	"fmt"
	"strings"
)

type keyValue struct {
	key   string
	value string
}

type Metadata struct {
	kvs []keyValue
}

func (m *Metadata) Add(k, v string) {
	m.kvs = append(m.kvs, keyValue{
		key:   k,
		value: v,
	})
}

func (m *Metadata) String() string {
	s := ";FFMETADATA1\n"
	for _, kv := range m.kvs {
		s += fmt.Sprintf("%s=%s\n", escape(kv.key), escape(kv.value))
	}
	return s
}

func escape(s string) string {
	rep := strings.NewReplacer(`=`, `\=`, `;`, `\;`, `#`, `\#`, "\n", "\\\n")
	return rep.Replace(s)
}
