package deploy_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/timothymamo/shift2eu/ci/sitedeploy/internal/deploy"
)

// The AWS documentation's published example of a SigV4-signed S3 GET Object
// request ("Authenticating Requests (AWS Signature Version 4)", the example of a
// GET object with a Range header): the access key, the secret key, the bucket,
// the key, the date, the region and the signature the page publishes. The
// values are the documentation's own, not a real key.
const (
	awsExampleAccessKey = "AKIAIOSFODNN7EXAMPLE"                     //nolint:gosec // G101: the AWS documentation's published example key, not a credential
	awsExampleSecretKey = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY" //nolint:gosec // G101: the AWS documentation's published example key, not a credential
	awsExampleSignature = "f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"

	// emptySHA256 and helloSHA256 are the published SHA-256 digests of the empty
	// body and of "hello".
	emptySHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	helloSHA256 = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
)

// TestHashPayload pins HashPayload against the published digests, the empty body
// a GET sends included.
func TestHashPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []byte
		want string
	}{
		{name: "a nil body", body: nil, want: emptySHA256},
		{name: "an empty body", body: []byte{}, want: emptySHA256},
		{name: "hello", body: []byte("hello"), want: helloSHA256},
		{name: "the bundle's module script", body: moduleBody, want: digest(moduleBody)},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := deploy.HashPayload(tc.body); got != tc.want {
				t.Errorf("HashPayload(%d bytes) = %q, want %q", len(tc.body), got, tc.want)
			}
		})
	}
}

// TestSignerSignsThePublishedS3Example pins Sign against the AWS
// documentation's published S3 GET Object example: the date and payload hash it
// sets, the signed headers the documented canonical request signs, and exactly
// the signature the page publishes. A SigV4 implementation that reproduces a
// published signature is the cheapest proof that its canonical request, scope
// and key chain are right.
func TestSignerSignsThePublishedS3Example(t *testing.T) {
	t.Parallel()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	request.Header.Set("Range", "bytes=0-9")

	signer := deploy.Signer{
		Credentials: deploy.Credentials{
			AccessKey: deploy.NewSecret(awsExampleAccessKey),
			SecretKey: deploy.NewSecret(awsExampleSecretKey),
		},
		Region:  "us-east-1",
		Service: deploy.ServiceS3,
	}

	at := time.Date(2013, time.May, 24, 0, 0, 0, 0, time.UTC)

	if err := signer.Sign(request, emptySHA256, at); err != nil {
		t.Fatalf("Sign of the documentation's example = %v, want nil", err)
	}

	if got, want := request.Header.Get("X-Amz-Date"), "20130524T000000Z"; got != want {
		t.Errorf("Sign set X-Amz-Date %q, want %q: the date is the clock in UTC as 20060102T150405Z", got, want)
	}

	if got := request.Header.Get("X-Amz-Content-Sha256"); got != emptySHA256 {
		t.Errorf("Sign set X-Amz-Content-Sha256 %q, want the payload hash %q it was given", got, emptySHA256)
	}

	want := "AWS4-HMAC-SHA256 " +
		"Credential=" + awsExampleAccessKey + "/20130524/us-east-1/s3/aws4_request, " +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date, " +
		"Signature=" + awsExampleSignature

	if got := request.Header.Get("Authorization"); got != want {
		t.Errorf("Sign set Authorization\n%q\nwant the documentation's published signature\n%q", got, want)
	}
}

// TestSignerRefusesWhatItCannotSign pins Sign's refusals: either half of the key,
// the region or the service missing, a payload hash that is not 64 lowercase
// hexadecimal characters, or a request with no host. Each returns an error, the
// request unchanged and the error holds neither key.
func TestSignerRefusesWhatItCannotSign(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		signer      deploy.Signer
		request     *http.Request
		payloadHash string
	}{
		{
			name:        "no access key",
			signer:      signer("", testSecretKey, testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: emptySHA256,
		},
		{
			name:        "no secret key",
			signer:      signer(testAccessKey, "", testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: emptySHA256,
		},
		{
			name:        "no region",
			signer:      signer(testAccessKey, testSecretKey, "", deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: emptySHA256,
		},
		{
			name:        "no service",
			signer:      signer(testAccessKey, testSecretKey, testRegion, ""),
			request:     exampleRequest(t),
			payloadHash: emptySHA256,
		},
		{
			name:        "a payload hash that is not hexadecimal",
			signer:      signer(testAccessKey, testSecretKey, testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: strings.Repeat("zz", 32),
		},
		{
			name:        "a payload hash of 63 characters",
			signer:      signer(testAccessKey, testSecretKey, testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: emptySHA256[:63],
		},
		{
			name:        "a payload hash in upper case",
			signer:      signer(testAccessKey, testSecretKey, testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: strings.ToUpper(emptySHA256),
		},
		{
			name:        "no payload hash",
			signer:      signer(testAccessKey, testSecretKey, testRegion, deploy.ServiceS3),
			request:     exampleRequest(t),
			payloadHash: "",
		},
		{
			name:   "a request with no host",
			signer: signer(testAccessKey, testSecretKey, testRegion, deploy.ServiceS3),
			request: &http.Request{
				Method: http.MethodGet,
				URL:    &url.URL{Path: "/test.txt"},
				Header: http.Header{},
			},
			payloadHash: emptySHA256,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.signer.Sign(tc.request, tc.payloadHash, fixedTime)
			if err == nil {
				t.Fatalf("Sign for %s = nil, want an error", tc.name)
			}

			if got := tc.request.Header.Get("Authorization"); got != "" {
				t.Errorf("Sign for %s set Authorization %q on a request it refused, want the request unchanged", tc.name, got)
			}

			for _, key := range []string{testAccessKey, testSecretKey} {
				if strings.Contains(err.Error(), key) {
					t.Errorf("the error reads %q (the key %q), want neither half of the key: a signing error names the request, never the key", err, key)
				}
			}
		})
	}
}

// signer returns a Signer of the given values.
func signer(accessKey, secretKey, region, service string) deploy.Signer {
	return deploy.Signer{
		Credentials: deploy.Credentials{
			AccessKey: deploy.NewSecret(accessKey),
			SecretKey: deploy.NewSecret(secretKey),
		},
		Region:  region,
		Service: service,
	}
}

// exampleRequest returns the request the documentation's example signs.
func exampleRequest(t *testing.T) *http.Request {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatalf("http.NewRequest: %v", err)
	}

	return request
}
