package awslambda

import (
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"

	"github.com/caddyserver/caddy/v2"
	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/caddyconfig/httpcaddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp/headers"
)

const (
	pluginName = "awslambda"
)

func init() {
	httpcaddyfile.RegisterHandlerDirective(pluginName, parseCaddyfile)
}

// parseCaddyfile sets up a handler for function execution
func parseCaddyfile(h httpcaddyfile.Helper) (caddyhttp.MiddlewareHandler, error) {
	var awsLambda AwsLambda
	// awsLambda.logger = initDebugLogger()
	err := awsLambda.UnmarshalCaddyfile(h.Dispenser)
	return awsLambda, err
}

// UnmarshalCaddyfile implements caddyfile.Unmarshaler.
func (awsLambda *AwsLambda) UnmarshalCaddyfile(d *caddyfile.Dispenser) error {

	// fmt.Fprintln(os.Stderr, d)

	for d.Next() {
		args := d.RemainingArgs()
		if len(args) > 0 {
			return d.ArgErr()
		}

		for d.NextBlock(0) {
			switch d.Val() {
			case "aws_secret":
				args = d.RemainingArgs()
				err := ensureArgsCount(d, args, 1)
				if err != nil {
					return err
				}
				awsLambda.AwsSecret = args[0]
			case "aws_access":
				args = d.RemainingArgs()
				err := ensureArgsCount(d, args, 1)
				if err != nil {
					return err
				}
				awsLambda.AwsAccess = args[0]
			case "aws_region":
				args = d.RemainingArgs()
				err := ensureArgsCount(d, args, 1)
				if err != nil {
					return err
				}
				awsLambda.AwsRegion = args[0]
			case "function_name":
				args = d.RemainingArgs()
				err := ensureArgsCount(d, args, 1)
				if err != nil {
					return err
				}
				awsLambda.FunctionName = args[0]
			case "header_up":
				var err error

				if awsLambda.Headers == nil {
					awsLambda.Headers = new(headers.Handler)
				}
				if awsLambda.Headers.Request == nil {
					awsLambda.Headers.Request = new(headers.HeaderOps)
				}
				args := d.RemainingArgs()

				switch len(args) {
				case 1:
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Request, args[0], "", aws.String(""))
				case 2:
					// some lint checks, I guess
					if strings.EqualFold(args[0], "host") && (args[1] == "{hostport}" || args[1] == "{http.request.hostport}") {
						caddy.Log().Named(pluginName).Warn("Unnecessary header_up Host: the reverse proxy's default behavior is to pass headers to the upstream")
					}
					if strings.EqualFold(args[0], "x-forwarded-for") && (args[1] == "{remote}" || args[1] == "{http.request.remote}" || args[1] == "{remote_host}" || args[1] == "{http.request.remote.host}") {
						caddy.Log().Named(pluginName).Warn("Unnecessary header_up X-Forwarded-For: the reverse proxy's default behavior is to pass headers to the upstream")
					}
					if strings.EqualFold(args[0], "x-forwarded-proto") && (args[1] == "{scheme}" || args[1] == "{http.request.scheme}") {
						caddy.Log().Named(pluginName).Warn("Unnecessary header_up X-Forwarded-Proto: the reverse proxy's default behavior is to pass headers to the upstream")
					}
					if strings.EqualFold(args[0], "x-forwarded-host") && (args[1] == "{host}" || args[1] == "{http.request.host}" || args[1] == "{hostport}" || args[1] == "{http.request.hostport}") {
						caddy.Log().Named(pluginName).Warn("Unnecessary header_up X-Forwarded-Host: the reverse proxy's default behavior is to pass headers to the upstream")
					}
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Request, args[0], args[1], aws.String(""))
				case 3:
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Request, args[0], args[1], aws.String(args[2]))
				default:
					return d.ArgErr()
				}

				if err != nil {
					return d.Err(err.Error())
				}
			case "header_down":
				var err error

				if awsLambda.Headers == nil {
					awsLambda.Headers = new(headers.Handler)
				}
				if awsLambda.Headers.Response == nil {
					awsLambda.Headers.Response = &headers.RespHeaderOps{
						HeaderOps: new(headers.HeaderOps),
					}
				}
				args := d.RemainingArgs()
				switch len(args) {
				case 1:
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Response.HeaderOps, args[0], "", aws.String(""))
				case 2:
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Response.HeaderOps, args[0], args[1], aws.String(""))
				case 3:
					err = headers.CaddyfileHeaderOp(awsLambda.Headers.Response.HeaderOps, args[0], args[1], aws.String(args[2]))
				default:
					return d.ArgErr()
				}

				if err != nil {
					return d.Err(err.Error())
				}

			default:
				return d.Errf("unsupported %s directive %q", pluginName, d.Val())
			}
		}
	}

	return nil
}

func (awsLambda *AwsLambda) ToAwsConfig() *aws.Config {
	awsConf := aws.NewConfig()
	if awsLambda.AwsRegion != "" {
		awsConf.WithRegion(awsLambda.AwsRegion)
	}
	if awsLambda.AwsAccess != "" {
		awsConf.WithCredentials(credentials.NewStaticCredentials(
			awsLambda.AwsAccess, awsLambda.AwsSecret, "",
		))
	}
	return awsConf
}

func ensureArgsCount(d *caddyfile.Dispenser, args []string, count int) error {
	if len(args) != count {
		return d.Errf("too many args %q, expected %d", args, count)
	}
	return nil
}

func ensureArgUint(d *caddyfile.Dispenser, name, arg string) (uint, error) {
	n, err := strconv.Atoi(arg)
	if err != nil {
		return 0, d.Errf("failed to convert %s %s: %v", name, arg, err)
	}
	ns := strconv.Itoa(n)
	if ns != arg {
		return 0, d.Errf("failed to convert %s %s, resolved %s", name, arg, ns)
	}
	if n < 0 {
		return 0, d.Errf("%s %s must be greater or equal to zero", name, arg)
	}

	return uint(n), nil
}
