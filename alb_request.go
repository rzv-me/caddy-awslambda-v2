package awslambda

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
)

func NewLambdaRequest(r *http.Request) (*events.ALBTargetGroupRequest, error) {
	buf := &bytes.Buffer{}
	if r.Body != nil {
		_, err := io.Copy(buf, r.Body)
		if err != nil {
			return nil, err
		}
	}

	queryParams, _ := url.ParseQuery(r.URL.RawQuery)

	headers := make(map[string][]string)
	for k, v := range r.Header {
		headers[strings.ToLower(k)] = v
	}

	headers["host"] = []string{r.Host}
	addForwardedHeaders(r, headers)

	request := &events.ALBTargetGroupRequest{
		HTTPMethod:                      r.Method,
		Path:                            r.URL.Path,
		MultiValueHeaders:               headers,
		MultiValueQueryStringParameters: queryParams,
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "",
			},
		},
		IsBase64Encoded: false,
		Body:            buf.String(),
	}

	return request, nil
}

func ClientIP(r *http.Request) string {
	address := caddyhttp.GetVar(r.Context(), caddyhttp.ClientIPVarKey).(string)
	clientIP, _, err := net.SplitHostPort(address)
	if err != nil {
		clientIP = address // no port
	}
	return clientIP
}

func addForwardedHeaders(r *http.Request, headers map[string][]string) {
	headers["x-forwarded-for"] = []string{ClientIP(r)}

	proto := "http"
	if r.TLS != nil {
		proto = "https"
	}
	headers["x-forwarded-proto"] = []string{proto}

	port := "80"
	if proto == "https" {
		port = "443"
	}
	headers["x-forwarded-port"] = []string{port}
}
