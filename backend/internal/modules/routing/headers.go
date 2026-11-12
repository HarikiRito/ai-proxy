package routing

import (
	"net/http"
	"strings"
)

// Never forwarded: the proxy's own credentials, values the transport owns, and hop-by-hop headers.
var skipHeaders = map[string]bool{
	"Authorization": true, "X-Api-Key": true, "Host": true, "Cookie": true, "Content-Length": true, "Accept-Encoding": true,
	"Connection": true, "Keep-Alive": true, "Proxy-Authenticate": true, "Proxy-Authorization": true,
	"Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
}

// forwardHeaders copies client headers to dst except the skipped ones and any named in Connection.
func forwardHeaders(dst, src http.Header) {
	named := map[string]bool{}
	for _, v := range src.Values("Connection") {
		for name := range strings.SplitSeq(v, ",") {
			named[http.CanonicalHeaderKey(strings.TrimSpace(name))] = true
		}
	}
	for k, vs := range src {
		if skipHeaders[k] || named[k] {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
