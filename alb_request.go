package awslambda

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/aws/aws-lambda-go/events"
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

	request := &events.ALBTargetGroupRequest{
		HTTPMethod:                      r.Method,
		Path:                            r.URL.Path,
		MultiValueHeaders:               headers,
		MultiValueQueryStringParameters: queryParams,
		RequestContext: events.ALBTargetGroupRequestContext{
			ELB: events.ELBContext{
				TargetGroupArn: "this-is-my-target-group",
			},
		},
		IsBase64Encoded: false,
		Body:            buf.String(),
	}

	return request, nil
}
