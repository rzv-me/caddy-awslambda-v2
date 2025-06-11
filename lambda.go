package awslambda

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/lambda"

	"go.uber.org/zap"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/headers"
)

func init() {
	caddy.RegisterModule(AwsLambda{})
}

type Invoker interface {
	Invoke(input *lambda.InvokeInput) (*lambda.InvokeOutput, error)
}

type AwsLambda struct {
	// Path this config block maps to
	Path string `json:"path,omitempty"`
	// AWS Access Key. If omitted, AWS_ACCESS_KEY_ID env var is used.
	AwsAccess string `json:"aws_access_key,omitempty"`
	// AWS Secret Key. If omitted, AWS_SECRET_ACCESS_KEY env var is used.
	AwsSecret string `json:"aws_secret_key,omitempty"`
	// AWS Region. If omitted, AWS_REGION env var is used.
	AwsRegion string `json:"aws_region,omitempty"`
	// If set, all requests to this path will invoke this function.
	FunctionName string `json:"function_name,omitempty"`

	Headers *headers.Handler `json:"headers,omitempty"`

	invoker Invoker

	logger *zap.SugaredLogger
}

// CaddyModule returns the Caddy module information.
func (AwsLambda) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "http.handlers.awslambda",
		New: func() caddy.Module { return new(AwsLambda) },
	}
}

func (awsLambda AwsLambda) ServeHTTP(w http.ResponseWriter, r *http.Request, next caddyhttp.Handler) error {
	// Define a shouldBuffer function - returns false to never buffer responses
	shouldBuffer := func(statusCode int, header http.Header) bool {
		return false // We don't need to buffer responses for AWS Lambda
	}

	// Use a response recorder to capture the response
	// In Caddy v2.9.1, NewResponseRecorder requires three parameters
	recorder := caddyhttp.NewResponseRecorder(w, nil, shouldBuffer)

	// Apply the header changes to the request
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)

	if awsLambda.Headers != nil && awsLambda.Headers.Request != nil {
		awsLambda.Headers.Request.ApplyToRequest(r)
	}

	// Create and prepare the Lambda request
	req, err := NewLambdaRequest(r)
	if err != nil {
		return caddyhttp.Error(http.StatusBadRequest, err)
	}

	payload, err := json.Marshal(req)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	invokeInput := &lambda.InvokeInput{
		FunctionName: aws.String(awsLambda.FunctionName),
		Payload:      payload,
	}

	invokeOutput, err := awsLambda.invoker.Invoke(invokeInput)
	if err != nil {
		return caddyhttp.Error(http.StatusBadGateway, err)
	}

	// Parse the Lambda response
	response, err := ParseLambdaResponse(invokeOutput)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	// Send the response
	return awsLambda.sendResponse(recorder, response, repl)
}

func (awsLambda AwsLambda) sendResponse(recorder http.ResponseWriter, response *events.ALBTargetGroupResponse, repl *caddy.Replacer) error {
	// Add response headers
	awsLambda.addResponseHeaders(recorder, response)

	// Set default status code if needed
	if response.StatusCode <= 0 {
		response.StatusCode = http.StatusOK
	}

	// Apply response headers from config
	if awsLambda.Headers != nil && awsLambda.Headers.Response != nil {
		awsLambda.Headers.Response.ApplyTo(recorder.Header(), repl)
	}

	// Write status code
	recorder.WriteHeader(response.StatusCode)

	if response.StatusCode == 204 {
		return nil
	}

	// Decode and write response body
	var bodyBytes []byte
	var err error
	if response.IsBase64Encoded && response.Body != "" {
		bodyBytes, err = base64.StdEncoding.DecodeString(response.Body)
		if err != nil {
			return caddyhttp.Error(http.StatusInternalServerError, err)
		}
	} else {
		bodyBytes = []byte(response.Body)
	}

	// Write the response body
	_, err = recorder.Write(bodyBytes)
	if err != nil {
		return caddyhttp.Error(http.StatusInternalServerError, err)
	}

	return nil
}

func (awsLambda AwsLambda) addResponseHeaders(recorder http.ResponseWriter, response *events.ALBTargetGroupResponse) {
	for k, v := range response.Headers {
		recorder.Header().Add(k, v)
	}

	for k, vals := range response.MultiValueHeaders {
		for _, v := range vals {
			recorder.Header().Add(k, v)
		}
	}

	if recorder.Header().Get("content-type") == "" {
		recorder.Header().Set("content-type", "application/json")
	}
}

func (awsLambda *AwsLambda) Provision(ctx caddy.Context) error {
	awsLambda.logger = ctx.Logger(awsLambda).Sugar()
	awsLambda.logger.Debug("AWS Lambda provisioned")
	sess, err := session.NewSession(awsLambda.ToAwsConfig())
	if err != nil {
		return err
	}
	awsLambda.invoker = lambda.New(sess)

	return nil
}

// Validate validates that the module has a usable config.
func (awsLambda AwsLambda) Validate() error {
	if awsLambda.FunctionName == "" {
		return fmt.Errorf("function_name is required")
	}

	// Check length constraints (1-170 characters)
	if len(awsLambda.FunctionName) < 1 || len(awsLambda.FunctionName) > 170 {
		return fmt.Errorf("function_name must be between 1 and 170 characters")
	}

	// AWS Lambda function name pattern
	// (arn:(aws[a-zA-Z-]*)?:lambda:)?([a-z]{2}(-gov)?-[a-z]+-\d{1}:)?(\d{12}:)?(function:)?([a-zA-Z0-9-_\.]+)(:(\$LATEST|[a-zA-Z0-9-_]+))?
	pattern := `^(arn:(aws[a-zA-Z-]*)?:lambda:)?([a-z]{2}(-gov)?-[a-z]+-\d{1}:)?(\d{12}:)?(function:)?([a-zA-Z0-9-_\.]+)(:(\$LATEST|[a-zA-Z0-9-_]+))?$`
	match, err := regexp.MatchString(pattern, awsLambda.FunctionName)
	if err != nil {
		return fmt.Errorf("error validating function_name pattern: %v", err)
	}
	if !match {
		return fmt.Errorf("invalid function_name format: must be a valid function name, ARN, or partial ARN")
	}

	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*AwsLambda)(nil)
	_ caddy.Validator             = (*AwsLambda)(nil)
	_ caddyhttp.MiddlewareHandler = (*AwsLambda)(nil)
	_ caddyfile.Unmarshaler       = (*AwsLambda)(nil)
)
