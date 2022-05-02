/*
Copyright (c) 2020 Red Hat, Inc.

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

// IMPORTANT: This file has been generated automatically, refrain from modifying it manually as all
// your changes will be lost when the file is generated again.

package v1 // github.com/openshift-online/ocm-sdk-go/authorizations/v1

import (
	"io"
	"net/http"
)

func readSelfTermsReviewPostRequest(request *SelfTermsReviewPostServerRequest, r *http.Request) error {
	var err error
	request.request, err = UnmarshalSelfTermsReviewRequest(r)
	return err
}
func writeSelfTermsReviewPostRequest(request *SelfTermsReviewPostRequest, writer io.Writer) error {
	return MarshalSelfTermsReviewRequest(request.request, writer)
}
func readSelfTermsReviewPostResponse(response *SelfTermsReviewPostResponse, reader io.Reader) error {
	var err error
	response.response, err = UnmarshalTermsReviewResponse(reader)
	return err
}
func writeSelfTermsReviewPostResponse(response *SelfTermsReviewPostServerResponse, w http.ResponseWriter) error {
	return MarshalTermsReviewResponse(response.response, w)
}
