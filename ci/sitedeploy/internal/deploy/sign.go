package deploy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
)

// ServiceS3 is the SigV4 service name of the S3 API, Scaleway Object
// Storage's included.
const ServiceS3 = "s3"

// The SigV4 names Sign uses.
const (
	sigV4Algorithm   = "AWS4-HMAC-SHA256"
	sigV4Terminator  = "aws4_request"
	amzDateLayout    = "20060102T150405Z"
	headerAmzDate    = "X-Amz-Date"
	headerAmzContent = "X-Amz-Content-Sha256"
	headerAuth       = "Authorization"
)

// hexDigest is a SHA-256 as Sign takes it: 64 lowercase hexadecimal
// characters.
var hexDigest = regexp.MustCompile(`^[0-9a-f]{64}$`)

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
	switch {
	case s.Credentials.AccessKey.IsZero() || s.Credentials.SecretKey.IsZero():
		return errors.New("sign: the key is incomplete")
	case s.Region == "" || s.Service == "":
		return errors.New("sign: no region or service")
	case !hexDigest.MatchString(payloadHash):
		return errors.New("sign: the payload hash is not 64 lowercase hexadecimal characters")
	case req == nil || req.URL == nil:
		return errors.New("sign: no request")
	}

	host := req.Host
	if host == "" {
		host = req.URL.Host
	}

	if host == "" {
		return errors.New("sign: the request has no host")
	}

	if req.Header == nil {
		req.Header = http.Header{}
	}

	amzDate := at.UTC().Format(amzDateLayout)
	date := amzDate[:8]

	req.Header.Del(headerAuth)
	req.Header.Set(headerAmzDate, amzDate)
	req.Header.Set(headerAmzContent, payloadHash)

	names, headers := canonicalHeaders(req.Header, host)

	path := req.URL.EscapedPath()
	if path == "" {
		path = "/"
	}

	canonical := strings.Join([]string{
		req.Method,
		path,
		canonicalQuery(req.URL.Query()),
		headers,
		names,
		payloadHash,
	}, "\n")

	scope := date + "/" + s.Region + "/" + s.Service + "/" + sigV4Terminator
	stringToSign := sigV4Algorithm + "\n" + amzDate + "\n" + scope + "\n" + HashPayload([]byte(canonical))

	key := hmacSHA256([]byte("AWS4"+s.Credentials.SecretKey.reveal()), date)
	key = hmacSHA256(key, s.Region)
	key = hmacSHA256(key, s.Service)
	key = hmacSHA256(key, sigV4Terminator)
	signature := hex.EncodeToString(hmacSHA256(key, stringToSign))

	req.Header.Set(headerAuth, sigV4Algorithm+
		" Credential="+s.Credentials.AccessKey.reveal()+"/"+scope+
		", SignedHeaders="+names+
		", Signature="+signature)

	return nil
}

// canonicalHeaders returns the signed headers' names, joined with ";", and
// the canonical headers, one "name:value\n" per header: host and every
// header of h, lowercased and sorted.
func canonicalHeaders(h http.Header, host string) (string, string) {
	values := map[string]string{"host": host}

	for name, vs := range h {
		lower := strings.ToLower(name)
		if lower == "host" {
			continue
		}

		folded := make([]string, 0, len(vs))
		for _, v := range vs {
			folded = append(folded, strings.Join(strings.Fields(v), " "))
		}

		values[lower] = strings.Join(folded, ",")
	}

	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}

	slices.Sort(names)

	var b strings.Builder
	for _, name := range names {
		b.WriteString(name + ":" + values[name] + "\n")
	}

	return strings.Join(names, ";"), b.String()
}

// canonicalQuery returns the query's names and values URI-encoded, sorted by
// name and then value, as name=value joined with "&".
func canonicalQuery(query url.Values) string {
	pairs := make([]string, 0, len(query))

	for name, values := range query {
		for _, value := range values {
			pairs = append(pairs, uriEncode(name)+"="+uriEncode(value))
		}
	}

	slices.Sort(pairs)

	return strings.Join(pairs, "&")
}

// uriEncode encodes every byte but the unreserved characters A-Z, a-z, 0-9,
// "-", "_", "." and "~" as %XX in upper case, as SigV4 defines it.
func uriEncode(s string) string {
	const upperHex = "0123456789ABCDEF"

	var b strings.Builder

	for i := range len(s) {
		c := s[i]

		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(upperHex[c>>4])
			b.WriteByte(upperHex[c&0x0f])
		}
	}

	return b.String()
}

// hmacSHA256 returns the HMAC-SHA256 of data under key.
func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(data))

	return mac.Sum(nil)
}

// HashPayload returns the 64 lowercase hexadecimal characters of the SHA-256
// of body, the payload hash Sign takes. HashPayload(nil) is the hash of the
// empty body, which a GET sends.
func HashPayload(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}
