/*
Copyright 2018 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package prow

import (
	"fmt"
	"regexp"
)

var pullCommitRe = regexp.MustCompile(`^[-\w]+:\w{40},\d+:(\w{40})$`)

// gets the pull commit hash from metadata
func getPullCommitHash(pull string) (string, error) {
	match := pullCommitRe.FindStringSubmatch(pull)
	if len(match) != 2 {
		expected := "branch:hash,pullNumber:hash"
		return "", fmt.Errorf("unable to parse pull %q (expected %q)", pull, expected)
	}
	return match[1], nil
}
