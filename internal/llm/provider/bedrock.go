package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

type bedrockOptions struct{}
type BedrockOption func(*bedrockOptions)
type bedrockProtocol struct{ model string }

func (b *bedrockProtocol) request(params object, stream bool) (string, any, error) {
	delete(params, "model")
	delete(params, "stream")
	params["anthropic_version"] = "bedrock-2023-05-31"
	method := "invoke"
	if stream {
		method = "invoke-with-response-stream"
	}
	return "/model/" + url.PathEscape(b.model) + "/" + method, params, nil
}
func newBedrockClient(opts providerClientOptions) (*anthropicClient, error) {
	region, err := bedrockRegion()
	if err != nil {
		return nil, err
	}
	model := opts.model.APIModel
	if strings.HasPrefix(model, "anthropic.") {
		prefix, _, _ := strings.Cut(region, "-")
		if prefix == "ap" {
			prefix = "apac"
		}
		model = prefix + "." + model
	}
	if !strings.Contains(model, "anthropic.") && !strings.HasPrefix(model, "arn:") {
		return nil, fmt.Errorf("unsupported model for bedrock provider: %s", model)
	}
	opts.model.APIModel = model
	opts.anthropicOptions = append(opts.anthropicOptions, WithAnthropicDisableCache())
	c := newAnthropicClient(opts)
	c.bedrock = &bedrockProtocol{model: model}
	suffix := "amazonaws.com"
	if strings.HasPrefix(region, "cn-") {
		suffix += ".cn"
	}
	transport := newAPITransport("https://bedrock-runtime." + region + "." + suffix)
	bearer := os.Getenv("AWS_BEARER_TOKEN_BEDROCK")
	if bearer != "" {
		transport.headers.Set("Authorization", "Bearer "+bearer)
	} else {
		cfg, err := awsconfig.LoadDefaultConfig(context.Background(), awsconfig.WithRegion(region))
		if err != nil {
			return nil, fmt.Errorf("load AWS credentials: %w", err)
		}
		signer := v4.NewSigner()
		transport.authorize = func(ctx context.Context, req *http.Request, body []byte) error {
			credentials, err := cfg.Credentials.Retrieve(ctx)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(body)
			return signer.SignHTTP(ctx, credentials, req, hex.EncodeToString(hash[:]), "bedrock", region, time.Now())
		}
	}
	// Bedrock's streaming content negotiation uses its own header, not SSE.
	transport.headers.Set("X-Amzn-Bedrock-Accept", "application/json")
	c.transport = transport
	return c, nil
}
func readBedrockEvents(r io.Reader, consume func(string, json.RawMessage) (bool, error)) error {
	decoder := eventstream.NewDecoder()
	for {
		frame, err := decoder.Decode(r, nil)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("decode Bedrock event: %w", err)
		}
		kind := ""
		if value := frame.Headers.Get(":message-type"); value != nil {
			kind = value.String()
		}
		chunk, err := decodeBedrockChunk(kind, frame.Payload)
		if err != nil {
			return err
		}
		stop, err := consume("", chunk)
		if stop || err != nil {
			return err
		}
	}
}

var bedrockRegionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]+)+-\d+$`)

func bedrockRegion() (string, error) {
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = os.Getenv("AWS_DEFAULT_REGION")
	}
	if region == "" {
		region = "us-east-1"
	}
	if !bedrockRegionPattern.MatchString(region) {
		return "", fmt.Errorf("invalid AWS region")
	}

	return region, nil
}

func decodeBedrockChunk(kind string, payload []byte) ([]byte, error) {
	if kind == "exception" || kind == "error" {
		return nil, fmt.Errorf("provider Bedrock stream exception: %s", payload)
	}
	var envelope struct {
		Bytes []byte `json:"bytes"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Bytes) == 0 {
		return nil, fmt.Errorf("provider Bedrock stream returned an empty chunk")
	}

	return envelope.Bytes, nil
}
