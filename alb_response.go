package awslambda

import (
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-sdk-go/service/lambda"
)

func ParseLambdaResponse(invokeOutput *lambda.InvokeOutput) (*events.ALBTargetGroupResponse, error) {
	if len(invokeOutput.Payload) > 0 && invokeOutput.Payload[0] == '{' {
		var response events.ALBTargetGroupResponse
		err := json.Unmarshal(invokeOutput.Payload, &response)
		return &response, err
	}

	return &events.ALBTargetGroupResponse{
		StatusCode:        500,
		StatusDescription: "Can't decode Lambda response",
		Headers:           nil,
		MultiValueHeaders: nil,
		Body:              string(invokeOutput.Payload),
		IsBase64Encoded:   false,
	}, nil
}
