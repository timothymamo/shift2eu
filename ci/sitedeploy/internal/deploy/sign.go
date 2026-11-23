package deploy

import (
	"net/http"
	"time"
)

// ServiceS3 is the SigV4 service name of the S3 API, Scaleway Object
// Storage's included.
const ServiceS3 = "s3"

// Signer signs requests with AWS Signature Version 4 (header form, not a
// presigned URL) for one region and service.
type Signer struct {
	// Credentials is the key the requests are signed with.
	Credentials Credentials
	// Region is the signing region, such as "fr-par".
	Region string
	// Service is the signing service, ServiceS3 for Object Storage.
	Service string
}

// Sign signs req in place, as AWS Signature Version 4 defines it:
//
//   - it sets X-Amz-Date to at in UTC (20060102T150405Z) and
//     X-Amz-Content-Sha256 to payloadHash, which must be the 64 lowercase
//     hexadecimal characters of the SHA-256 of the body (HashPayload);
//   - the canonical request is the method, req.URL.EscapedPath() as it is
//     (S3 encodes the path once, never twice; "/" when empty), the query
//     with its names and values URI-encoded and sorted, the canonical
//     headers and the signed headers, and payloadHash. The signed headers
//     are host (req.Host, else req.URL.Host) and every header present in
//     req.Header when Sign is called, lowercased, sorted, values trimmed and
//     inner runs of spaces folded to one, several values of one header
//     joined with ",";
//   - the scope is <date>/<Region>/<Service>/aws4_request, the signing key
//     the HMAC-SHA256 chain from "AWS4"+secret key, and it sets
//     Authorization to "AWS4-HMAC-SHA256 Credential=<access key>/<scope>,
//     SignedHeaders=<names>, Signature=<hex>".
//
// It returns an error, and leaves req unchanged, when either key or the
// region or service is empty, payloadHash is not 64 lowercase hexadecimal
// characters, or req has no host. Its error never holds either key.
// Headers the HTTP client adds when it sends (User-Agent, Content-Length,
// Accept-Encoding) are not in req.Header, so they are not signed.
func (s Signer) Sign(req *http.Request, payloadHash string, at time.Time) error {
	_, _, _ = req, payloadHash, at

	return ErrNotImplemented
}

// HashPayload returns the 64 lowercase hexadecimal characters of the SHA-256
// of body, the payload hash Sign takes. HashPayload(nil) is the hash of the
// empty body, which a GET sends.
func HashPayload(body []byte) string {
	_ = body

	return ""
}
