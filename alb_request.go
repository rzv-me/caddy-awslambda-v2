package awslambda

import (
	"bytes"
	"io"
	"net"
	"net/http"
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

	queryParams := createQueryParameters(r)
	headers := createHeaders(r)

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

func createQueryParameters(r *http.Request) map[string][]string {
	queryParams := make(map[string][]string)
	for _, param := range strings.Split(r.URL.RawQuery, "&") {
		key, value, _ := strings.Cut(param, "=")
		if key == "" {
			continue
		}
		queryParams[key] = append(queryParams[key], value)
	}
	return queryParams
}

func createHeaders(r *http.Request) map[string][]string {
	headers := make(map[string][]string)
	for k, v := range r.Header {
		headers[strings.ToLower(k)] = v
	}

	headers["host"] = []string{r.Host}
	addForwardedHeaders(r, headers)
	return headers
}
func addForwardedHeaders(r *http.Request, headers map[string][]string) {
	headers["x-forwarded-for"] = []string{clientIP(r)}

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

func clientIP(r *http.Request) string {
	address := caddyhttp.GetVar(r.Context(), caddyhttp.ClientIPVarKey).(string)
	clientIP, _, err := net.SplitHostPort(address)
	if err != nil {
		clientIP = address // no port
	}
	return clientIP
}
