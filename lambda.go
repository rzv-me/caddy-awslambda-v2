package awslambda

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

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
	// httpcaddyfile.RegisterHandlerDirective("awslambda", parseCaddyfile)
}

type Invoker interface {
	Invoke(input *lambda.InvokeInput) (*lambda.InvokeOutput, error)
}

type AwsLambda struct {
	// Path this config block maps to
	Path string `json:"path,omitempty"`
	// AWS Access Key. If omitted, AWS_ACCESS_KEY_ID env var is used.
	AwsAccess string `json:"aws_access,omitempty"`
	// AWS Secret Key. If omitted, AWS_SECRET_ACCESS_KEY env var is used.
	AwsSecret string `json:"aws_secret,omitempty"`
	// AWS Region. If omitted, AWS_REGION env var is used.
	AwsRegion string `json:"aws_region,omitempty"`
	// If set, all requests to this path will invoke this function.
	FunctionName string `json:"function_name,omitempty"`

	// Headers manipulates headers between Caddy and the backend.
	// By default, all headers are passed-thru without changes,
	// with the exceptions of special hop-by-hop headers.
	//
	// X-Forwarded-For, X-Forwarded-Proto and X-Forwarded-Host
	// are also set implicitly.
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
	// Use a response recorder to capture the response
	recorder := caddyhttp.NewResponseRecorder(w, nil, nil)

	// Apply the header changes to the request
	repl := r.Context().Value(caddy.ReplacerCtxKey).(*caddy.Replacer)

	if awsLambda.Headers.Request != nil {
		awsLambda.Headers.Request.ApplyToRequest(r)
	}

	functionName := awsLambda.FunctionName

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
		FunctionName: aws.String(functionName),
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

	// Set response headers
	for k, v := range response.Headers {
		recorder.Header().Add(k, v)
	}

	for k, vals := range response.MultiValueHeaders {
		for _, v := range vals {
			recorder.Header().Add(k, v)
		}
	}

	// Default the Content-Type if not provided
	if recorder.Header().Get("content-type") == "" {
		recorder.Header().Set("content-type", "application/json")
	}

	// Set default status code if needed
	if response.StatusCode <= 0 {
		response.StatusCode = http.StatusOK
	}

	// Apply response headers from config
	if awsLambda.Headers.Response != nil {
		awsLambda.Headers.Response.ApplyTo(recorder.Header(), repl)
	}

	// Write status code
	recorder.WriteHeader(response.StatusCode)

	// Decode and write response body
	var bodyBytes []byte
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

	// Important: If we handled the request successfully and don't want to
	// pass it to the next handler, we should return nil here
	return nil
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
	// TODO: validate the module's setup
	return nil
}

// Interface guards
var (
	_ caddy.Provisioner           = (*AwsLambda)(nil)
	_ caddy.Validator             = (*AwsLambda)(nil)
	_ caddyhttp.MiddlewareHandler = (*AwsLambda)(nil)
	_ caddyfile.Unmarshaler       = (*AwsLambda)(nil)
)
